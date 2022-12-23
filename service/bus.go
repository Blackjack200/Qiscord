package service

type Message interface {
	HandleDiscordMessage()
	HandleQQMessage()
}
