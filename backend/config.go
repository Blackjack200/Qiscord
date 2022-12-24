package backend

import "github.com/tuotoo/qrcode"

type ServiceData struct {
	HandleQrCode   func(*qrcode.Matrix)
	HandleCaptcha  func([]byte) string
	ReceiveSMSCode func() string
	Config         *Config
}

type Config struct {
	DiscordToken string `json:"discord-token"`
	Account      int64  `json:"account"`
	Method       string `json:"login-method"`
	Password     string `json:"password"`
}
