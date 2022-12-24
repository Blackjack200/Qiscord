package backend

type Config struct {
	DiscordToken string `json:"discord-token"`
	Account      int64  `json:"account"`
	Method       string `json:"login-method"`
	Password     string `json:"password"`
}
