package backend

import (
	"bufio"
	"encoding/json"
	"fmt"
	qrcodeTerminal "github.com/Baozisoftware/qrcode-terminal-go"
	"github.com/Blackjack200/Qiscord/storage"
	"github.com/Blackjack200/Qiscord/util"
	"github.com/Logiase/MiraiGo-Template/bot"
	"github.com/Logiase/MiraiGo-Template/bot/device"
	"github.com/bwmarrin/discordgo"
	"github.com/sirupsen/logrus"
	"github.com/tuotoo/qrcode"
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

func discordLogin(cfg *Config) (*discordgo.Session, error) {
	d, _ := discordgo.New("Bot " + cfg.DiscordToken)
	return d, d.Open()
}

func qqLogin(log *logrus.Logger, cfg *Config) (*bot.Bot, error) {
	if !util.FileExists("./device.json") {
		w := util.MustOpenFile("./device.json")
		util.Must(device.GenRandomDeviceWriter(w), w.Close())
	}

	if !util.FileExists("./config.json") {
		w := util.MustOpenFile("./config.json")
		util.Must(json.NewEncoder(w).Encode(Config{
			DiscordToken: "",
			Account:      123456,
			Method:       bot.LoginMethodCommon,
			Password:     "123456",
		}), w.Close())
	}

	deviceJson := util.MustReadFile("./device.json")

	lg := &bot.Loginer{
		Log:               log,
		Protocol:          device.AndroidWatch,
		Method:            bot.LoginMethod(cfg.Method),
		DeviceJSONContent: deviceJson,
		Account:           cfg.Account,
		Password:          cfg.Password,
		ReadTokenCache: func() ([]byte, bool) {
			c, err := util.ReadFile("./session.dat")
			return c, err == nil
		},
		HandleQrCode: func(matrix *qrcode.Matrix) {
			qrcodeTerminal.New().Get(matrix.Content).Print()
		},
		ReceiveSMSCode: func() string {
			fmt.Printf("Input Your SMS Code: ")
			sc := bufio.NewScanner(os.Stdin)
			if sc.Scan() {
				return sc.Text()
			}
			util.Must(sc.Err())
			return ""
		},
		HandleCaptcha: func(i []byte) string {
			panic("not implemented.")
		},
	}

	b, err := lg.Login()

	if err != nil {
		return nil, err
	}

	util.Must(os.WriteFile("./session.dat", b.GenToken(), 0666))
	return b, nil
}
