package downloader

import (
	"context"
	"fmt"
	"github.com/pcmid/waifud/core"
	"github.com/pcmid/waifud/services"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
	"github.com/zyxar/argo/rpc"
	"strconv"
	"strings"
	"time"
)

func init() {
	services.ServiceMap["aria2c"] = &Aria2c{}
}

type Aria2c struct {
	rpcUrl    string
	rpcSecret string

	missions map[string]*Mission
	sms      chan core.Message
	rms      chan *Mission
}

type Mission struct {
	Gid          string
	Name         string
	Status       string
	ProgressRate float64
	FollowedBy   []string
}

func (a *Aria2c) Name() string {
	return "aria2c"
}

func (a *Aria2c) ListeningTypes() []string {
	return []string{
		"enclosure",
		"api",
	}
}

func (a *Aria2c) Init() {

	a.rpcUrl = "http://127.0.0.1:6800/jsonrpc"
	a.rpcSecret = ""

	a.missions = make(map[string]*Mission)

	a.rms = make(chan *Mission)

	if viper.IsSet("service.aria2c.url") {
		a.rpcUrl = viper.GetString("service.aria2c.url")
		log.Tracef("set aria2c rpc url %s", a.rpcUrl)
	} else {
		log.Warnf("aria2 rpc url not found, use %s", a.rpcUrl)
	}

	if viper.IsSet("service.aria2c.secret") {
		a.rpcSecret = viper.GetString("service.aria2c.secret")
		log.Tracef("set aria2c rpc secret %s", a.rpcSecret)

	} else {
		log.Warnf("aria2 rpc secret not found, use \"%s\"", a.rpcSecret)
	}
}

func (a *Aria2c) SetMessageChan(ms chan core.Message) {
	a.sms = ms
}

func (a *Aria2c) Send(message core.Message) {
	a.sms <- message
}

func (a *Aria2c) Serve() {
	tick := time.NewTicker(2 * time.Second)

	for {
		select {
		case <-tick.C:
			a.UpdateStatus()

			for gid, m := range a.missions {
				switch m.Status {
				case "complete":
					log.Infof("%s download completed", m.Name)
					if followed := m.FollowedBy; followed != nil {
						for _, g := range followed {
							a.missions[g] = &Mission{
								Gid: g,
							}
						}
					} else {
						a.Send(core.Message{
							Type: "notify",
							Msg:  m.Name,
						})
					}
					delete(a.missions, gid)

				case "error":
					a.Send(core.Message{
						Type: "notify",
						Msg:  fmt.Sprintf("%s 下载失败", m.Name),
					})
					delete(a.missions, gid)

				case "removed":
					delete(a.missions, gid)
				}
			}
		case mission := <-a.rms:
			a.missions[mission.Gid] = mission
		}
	}
}

func (a *Aria2c) Handle(message core.Message) {

	switch message.Type {
	case "api":
		method := message.Message().(string)
		switch method {
		case "status":
			//a.UpdateStatus()
			a.Send(core.Message{
				Type: "status",
				Msg:  a.missions,
			})
		}
	case "enclosure":
		url := message.Message().(string)
		a.Download(url)
	}
}

func (a *Aria2c) Download(url string) {
	log.Infof("%s Download %s", a.Name(), url)

	rpcc, err := rpc.New(context.Background(), a.rpcUrl, a.rpcSecret, time.Second, nil)
	defer func() {
		_ = rpcc.Close()
	}()

	if err != nil {
		log.Errorf("%s Failed to connect aria2 rpc server: %s", a.Name(), err)
		return
	}

	gid, err := rpcc.AddURI(url, rpc.Option{})

	if err != nil {
		log.Errorf("%s Failed to AddURL for aria2c: %s", a.Name(), err)
		return
	}

	//log.Trace(Gid)

	a.rms <- &Mission{
		Gid: gid,
	}
}

func (a *Aria2c) UpdateStatus() {
	rpcc, _ := rpc.New(context.Background(), a.rpcUrl, a.rpcSecret, time.Second, nil)

	for gid, mission := range a.missions {
		s, _ := rpcc.TellStatus(gid)
		mission.Status = s.Status
		mission.FollowedBy = s.FollowedBy
		if s.InfoHash == "" {
			uris, _ := rpcc.GetURIs(gid)
			if len(uris) > 0 {
				mission.Name = uris[0].URI[strings.LastIndex(uris[0].URI, "/")+1:]
			}
		} else {
			mission.Name = s.BitTorrent.Info.Name
		}

		completedLength, _ := strconv.ParseFloat(s.CompletedLength, 10)
		totalLength, _ := strconv.ParseFloat(s.TotalLength, 10)

		if s.TotalLength == "0" {
			mission.ProgressRate = 0
			return
		}
		mission.ProgressRate = completedLength / totalLength
	}
}
