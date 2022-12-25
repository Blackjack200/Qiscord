package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	qrcodeTerminal "github.com/Baozisoftware/qrcode-terminal-go"
	"github.com/Blackjack200/Qiscord/backend"
	"github.com/Blackjack200/Qiscord/util"
	"github.com/Logiase/MiraiGo-Template/bot"
	"github.com/sirupsen/logrus"
	"github.com/tuotoo/qrcode"
	"os"
	"os/signal"
)

func main() {
	log := logrus.New()
	log.SetFormatter(&logrus.TextFormatter{ForceColors: true})
	util.ErrorFunc(func(v interface{}) {
		log.Error(v)
	})
	util.PanicFunc(func(v interface{}) {
		log.Panic(v)
	})
	cfg, err := readConfig()
	if err != nil {
		log.Fatalf("read config: %v", err)
	}
	s, err := backend.NewService(log, &backend.ServiceData{
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
		Config: cfg,
	})
	if err != nil {
		log.Fatalf("new service: %v", err)
	}
	//frontend

	util.Must(s.Start("1047674480677371964"))

	wait()
	util.Must(s.Stop())
}

func wait() {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt, os.Kill)
	<-ch
}

func readConfig() (*backend.Config, error) {
	if !util.FileExists("./config.json") {
		w := util.MustOpenFile("./config.json")
		util.Must(json.NewEncoder(w).Encode(&backend.Config{
			DiscordToken: "",
			Account:      123456,
			Method:       bot.LoginMethodCommon,
			Password:     "123456",
		}), w.Close())
	}

	configJson := util.MustReadFile("./config.json")
	config := &backend.Config{}
	return config, json.Unmarshal(configJson, config)
}
