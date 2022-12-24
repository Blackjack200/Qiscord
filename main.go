package main

import (
	"encoding/json"
	"github.com/Blackjack200/Qiscord/backend"
	"github.com/Blackjack200/Qiscord/util"
	"github.com/bwmarrin/discordgo"
	"github.com/sirupsen/logrus"
	"os"
	"os/signal"
	"strings"
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
	s, err := backend.NewService(log, cfg)
	if err != nil {
		log.Fatalf("new service: %v", err)
	}
	//frontend
	started := false
	s.Discord().AddHandler(func(discord *discordgo.Session, discordMsg *discordgo.MessageCreate) {
		if started {
			return
		}
		guildID := discordMsg.GuildID
		channelID := discordMsg.ChannelID
		if strings.EqualFold(discordMsg.Content, "clear") {
			util.Must(s.DeleteALlChannel(guildID))
			util.Must(discord.ChannelMessageSend(channelID, "ok"))
		}

		if strings.EqualFold(discordMsg.Content, "ping") {
			util.Must(s.Start(guildID))
			util.Must(discord.ChannelMessageSend(channelID, "ok"))
		}
	})

	wait()
	util.Must(s.Stop())
}

func wait() {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt, os.Kill)
	<-ch
}

func readConfig() (*backend.Config, error) {
	configJson := util.MustReadFile("./config.json")
	config := &backend.Config{}
	return config, json.Unmarshal(configJson, config)
}
