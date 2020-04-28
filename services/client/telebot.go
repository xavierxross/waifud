package client

import (
	"fmt"
	"github.com/pcmid/waifud/core"
	"github.com/pcmid/waifud/services"
	"github.com/pcmid/waifud/services/database"
	"github.com/pcmid/waifud/services/downloader"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
	tb "gopkg.in/tucnak/telebot.v2"
	"os"
	"strings"
	"time"
)

func init() {
	services.ServiceMap["telebot"] = &TeleBot{}
}

type TeleBot struct {
	bot *tb.Bot

	rms chan core.Message
	sms chan core.Message

	chat tb.Recipient
}

func (t *TeleBot) Name() string {
	return "telebot"
}

func (t *TeleBot) ListeningTypes() []string {
	return []string{
		"feeds",
		"notify",
		"status",
	}
}

func (t *TeleBot) Init() {
	token := viper.GetString("service.TeleBot.token")

	if token == "" {
		log.Error("TeleBot token not found, exit")
		os.Exit(-1)
	}

	log.Tracef("set telebot token %s", token)

	b, err := tb.NewBot(tb.Settings{
		// the token just for test
		Token:  token,
		Poller: &tb.LongPoller{Timeout: 10 * time.Second},
	})

	if err != nil {
		log.Errorf("Failed to init telebot: %s", err)
		b = t.initAfterFailed(token)
	}

	b.Handle("/ping", func(m *tb.Message) {
		_, _ = b.Send(m.Sender, "pong!")
	})

	b.Handle("/sub", t.commandSub)
	b.Handle("/unsub", t.commandUnSub)
	b.Handle("/getsub", t.commandGetSub)
	b.Handle("/link", t.commandLink)
	b.Handle("/status", t.commandStatus)

	t.bot = b
}

func (t *TeleBot) Serve() {
	if t.bot == nil {
		log.Errorf("Failed to start %s", t.Name())
		return
	}
	t.bot.Start()
}

func (t *TeleBot) Handle(message core.Message) {
	if t.chat == nil {
		return
	}

	switch message.Type {
	case "notify":
		go t.Notify(message.Message().(string), false)
	case "feeds":
		feeds := message.Message().(map[string]*database.Feed)
		if len(feeds) == 0 {
			go t.Notify("未找到订阅", false)
			return
		}

		resp := strings.Builder{}
		resp.WriteString("订阅如下:\n")

		for _, feed := range feeds {
			resp.WriteString(fmt.Sprintf("[%s](%s)\n", feed.Title, feed.URL))
		}

		go t.Notify(resp.String(), true)

	case "status":
		statues := message.Message().(map[string]*downloader.Mission)
		if len(statues) == 0 {
			go t.Notify("未找到下载项目", false)
			return
		}

		resp := strings.Builder{}
		resp.WriteString("正在下载:\n")

		for _, status := range statues {
			resp.WriteString(fmt.Sprintf("名称: %s\n\t状态: %s\n\t进度: %.2f%%\n", status.Name, status.Status, status.ProgressRate*100))
		}

		go t.Notify(resp.String(), false)
	}
}

func (t *TeleBot) Notify(m string, isMarkDown bool) {
	retryTimes := 10
	tc := time.Tick(30 * time.Second)

	opt := &tb.SendOptions{
		ReplyTo:               nil,
		ReplyMarkup:           nil,
		DisableWebPagePreview: true,
		DisableNotification:   false,
		ParseMode:             "",
	}

	if isMarkDown {
		opt.ParseMode = tb.ModeMarkdown
	}

	for {
		if retryTimes == 0 {
			return
		}
		if _, e := t.bot.Send(t.chat, m, opt); e == nil {
			return
		} else {
			log.Errorf("Failed to send message: %s, retrying...", e)
			retryTimes--
		}
		<-tc
	}
}

func (t *TeleBot) commandSub(m *tb.Message) {
	url := m.Payload
	if url == "" {
		_, _ = t.bot.Send(m.Sender, "usage :/sub URL")
		return
	}
	log.Trace(url)

	t.chat = m.Sender

	t.Send(core.Message{
		Type: "subscription",
		Msg: &database.Subscription{
			Op:  database.Sub,
			Url: url,
		},
	})
}

func (t *TeleBot) commandUnSub(m *tb.Message) {
	url := m.Payload
	if url == "" {
		_, _ = t.bot.Send(m.Sender, "usage :/unsub URL")
		return
	}
	log.Trace(url)

	t.chat = m.Sender

	t.Send(core.Message{
		Type: "subscription",
		Msg: &database.Subscription{
			Op:  database.UnSub,
			Url: url,
		},
	})
}

func (t *TeleBot) commandGetSub(m *tb.Message) {
	t.chat = m.Sender

	t.Send(core.Message{
		Type: "subscription",
		Msg: &database.Subscription{
			Op:  database.GetSub,
			Url: "",
		},
	})
}

func (t *TeleBot) commandLink(m *tb.Message) {
	t.chat = m.Sender

	link := m.Payload

	t.Send(core.Message{
		Type: "enclosure",
		Msg:  link,
	})
}

func (t *TeleBot) commandStatus(m *tb.Message) {
	t.chat = m.Sender

	t.Send(core.Message{
		Type: "api",
		Msg:  "status",
	})
}

func (t *TeleBot) initAfterFailed(token string) *tb.Bot {
	tc := time.Tick(30 * time.Second)
	for {
		<-tc
		b, err := tb.NewBot(tb.Settings{
			Token:  token,
			Poller: &tb.LongPoller{Timeout: 10 * time.Second},
		})

		if err == nil {
			log.Info("Init telebot successfully")
			return b
		}
	}
}

func (t *TeleBot) SetMessageChan(ms chan core.Message) {
	t.sms = ms
}

func (t *TeleBot) Send(message core.Message) {
	t.sms <- message
}
