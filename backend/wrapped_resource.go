package backend

import (
	"github.com/Blackjack200/Qiscord/storage"
	"github.com/Blackjack200/Qiscord/util"
	"github.com/Logiase/MiraiGo-Template/bot"
	"github.com/Logiase/MiraiGo-Template/bot/device"
	"github.com/bwmarrin/discordgo"
	"github.com/sirupsen/logrus"
	"io"
	"os"
)

func messageHistory() (storage.MessageHistory, func() error, error) {
	r := util.MustInitFile("./message.dat")

	h, err := storage.ReadMessageHistory(r)
	util.Must(r.Close())

	if err != nil && err != io.EOF {
		return nil, nil, err
	}

	return h, func() error {
		f := util.MustOpenFile("./message.dat")
		return util.AnyError(h.Save(f), f.Close())
	}, nil
}

func discordLogin(data *ServiceData) (*discordgo.Session, error) {
	d, _ := discordgo.New("Bot " + data.Config.DiscordToken)
	return d, d.Open()
}

func qqLogin(log *logrus.Logger, data *ServiceData) (*bot.Bot, error) {
	if !util.FileExists("./device.json") {
		w := util.MustOpenFile("./device.json")
		util.Must(device.GenRandomDeviceWriter(w), w.Close())
	}
	deviceJson := util.MustReadFile("./device.json")

	lg := &bot.Loginer{
		Log:               log,
		Protocol:          device.AndroidWatch,
		Method:            bot.LoginMethod(data.Config.Method),
		DeviceJSONContent: deviceJson,
		Account:           data.Config.Account,
		Password:          data.Config.Password,
		ReadTokenCache: func() ([]byte, bool) {
			c, err := util.ReadFile("./session.dat")
			return c, err == nil
		},
		HandleQrCode:   data.HandleQrCode,
		ReceiveSMSCode: data.ReceiveSMSCode,
		HandleCaptcha:  data.HandleCaptcha,
	}

	b, err := lg.Login()

	if err != nil {
		return nil, err
	}

	util.Must(os.WriteFile("./session.dat", b.GenToken(), 0666))
	return b, nil
}
