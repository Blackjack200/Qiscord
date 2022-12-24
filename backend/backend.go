package backend

import (
	"github.com/sirupsen/logrus"
)
import (
	"fmt"
	"github.com/Blackjack200/Qiscord/storage"
	"github.com/Blackjack200/Qiscord/util"
	"github.com/Logiase/MiraiGo-Template/bot"
	"github.com/Mrs4s/MiraiGo/client"
	"github.com/bwmarrin/discordgo"
	"strconv"
	"strings"
	"sync"
)

type Service struct {
	log             *logrus.Logger
	discord         *discordgo.Session
	qq              *bot.Bot
	history         storage.MessageHistory
	saveHistoryFunc func() error

	initTransportOnce bool
	qqToDiscordMap    map[int64]*discordgo.Channel
	discordToQQMap    map[string]int64
	registerFunc      func(groupCode int64, groupName string) error
}

func NewService(log *logrus.Logger, data *ServiceData) (*Service, error) {
	discord, qq, history, saveHistoryFunc, err := login(log, data)
	if err != nil {
		return nil, err
	}
	return &Service{
		log:             log,
		discord:         discord,
		qq:              qq,
		history:         history,
		saveHistoryFunc: saveHistoryFunc,
	}, nil
}

func login(log *logrus.Logger, data *ServiceData) (*discordgo.Session, *bot.Bot, storage.MessageHistory, func() error, error) {
	d, err := discordLogin(data)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("failed login discord: %v", err)
	}

	b, err := qqLogin(log, data)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("failed login qq: %v", err)
	}

	h, saveFunc, err := messageHistory()
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("failed load message history: %v", err)
	}

	b.RefreshList()
	return d, b, h, saveFunc, nil
}

func (s *Service) Start(guildId string) error {
	err := s.initTransport(guildId)
	if err != nil {
		return err
	}
	s.syncHistoryMessage()
	s.initHandler()
	return nil
}

func (s *Service) initTransport(guildID string) (err error) {
	if s.initTransportOnce {
		return
	}
	s.initTransportOnce = true
	s.qqToDiscordMap, s.discordToQQMap, s.registerFunc, err = initTransportImpl(s.discord, s.qq, guildID)
	return
}

func (s *Service) DeleteALlChannel(guildId string) error {
	c, err := s.discord.GuildChannels(guildId)
	if err != nil {
		return err
	}
	for _, cc := range c {
		if len(strings.Split(cc.Name, "_")) >= 2 {
			_, err = s.discord.ChannelDelete(cc.ID)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

// dumpTransports for debug uses
//
//goland:noinspection GoUnusedFunction
func dumpTransports(d *Service, channelId string) {
	buf := ""
	for n, r := range d.qq.GroupList {
		if c, ok := d.qqToDiscordMap[r.Code]; ok {
			buf += fmt.Sprintf("%v\n", c.Mention())
			if n%24 == 0 {
				util.Optional(d.discord.ChannelMessageSend(channelId, buf))
				buf = ""
			}
		}
	}
	util.Optional(d.discord.ChannelMessageSend(channelId, buf))
}

func (s *Service) syncHistoryMessage() {
	defer func() {
		panicThing := recover()
		if panicThing != nil {
			s.log.Errorf("panic: sync history message: %v", panicThing)
		}
	}()
	wg := &sync.WaitGroup{}

	s.log.Infof("syncing history message")

	for groupCode, channel := range s.qqToDiscordMap {
		//confusing :p
		groupCode := groupCode
		channel := channel

		wg.Add(1)
		go func() {
			defer func() {
				panicThing := recover()
				if panicThing != nil {
					s.log.Errorf("panic: sync history message: %v", panicThing)
				}
			}()
			defer wg.Done()

			lastId, ok := s.history.LastId(channel.GuildID, channel.ID)
			groupInfo, err := s.qq.GetGroupInfo(groupCode)
			util.Must(err)

			if ok && groupInfo != nil {
				//we don't care this error
				msgs, _ := s.qq.GetGroupMessages(groupCode, lastId+1, groupInfo.LastMsgSeq)
				if len(msgs) > 0 {
					s.log.Infof(" - Syncing %v", groupInfo.Name)

					for i, msg := range msgs {
						s.handleQQMessage(channel, msg, true)
						s.log.Infof(" - Syncing %v (%v/%v)", groupInfo.Name, i+1, len(msgs))
					}
				}
			}
		}()
	}
	wg.Wait()
	s.log.Infof("synced history message")
}

func (s *Service) channelToGroup(channelId string) (*client.GroupInfo, bool) {
	a, ok := s.discordToQQMap[channelId]
	if !ok {
		return nil, false
	}
	group := s.qq.FindGroup(a)
	if group == nil {
		return nil, false
	}
	return group, true
}

func (s *Service) channelToGroupWithQuery(channelId string) (*client.GroupInfo, bool) {
	a, ok := s.discordToQQMap[channelId]
	if !ok {
		return nil, false
	}
	group, err := s.qq.GetGroupInfo(a)
	util.Must(err)
	if group == nil {
		return nil, false
	}
	return group, true
}

func (s *Service) groupToChannelWithQuery(groupCode int64) (*discordgo.Channel, bool) {
	channelId, ok := s.qqToDiscordMap[groupCode]
	if !ok {
		return nil, false
	}
	channel, err := s.discord.Channel(channelId.ID)
	util.Must(err)
	if channel == nil {
		return nil, false
	}
	return channel, true
}

func (s *Service) groupToChannel(groupCode int64) (*discordgo.Channel, bool) {
	channelId, ok := s.qqToDiscordMap[groupCode]
	if !ok {
		return nil, false
	}
	return channelId, ok
}

func (s *Service) Stop() error {
	s.qq.Release()
	return util.AnyError(s.saveHistoryFunc(), s.discord.Close())
}

// TODO remove this
func initTransportImpl(d *discordgo.Session, b *bot.Bot, guildId string) (map[int64]*discordgo.Channel, map[string]int64, func(groupCode int64, groupName string) error, error) {
	cs, err := d.GuildChannels(guildId)
	if err != nil {
		return nil, nil, nil, err
	}

	QQToDiscordMap := make(map[int64]*discordgo.Channel)
	discordToQQMap := make(map[string]int64)

	for _, c := range cs {
		splits := strings.Split(c.Name, "_")
		parts := splits[len(splits)-1]
		for _, g := range b.GroupList {
			if parts == strconv.FormatInt(g.Code, 10) {
				QQToDiscordMap[g.Code] = c
				discordToQQMap[c.ID] = g.Code
				break
			}
		}
		// TODO delete quited groups
	}

	register := func(groupCode int64, groupName string) error {
		if _, ok := QQToDiscordMap[groupCode]; !ok {
			c, err := d.GuildChannelCreateComplex(guildId, discordgo.GuildChannelCreateData{
				Name:  fmt.Sprintf("%v_%v", groupName, groupCode),
				Type:  discordgo.ChannelTypeGuildText,
				Topic: "qq",
			})
			if err != nil {
				return err
			}
			QQToDiscordMap[groupCode] = c
			discordToQQMap[c.ID] = groupCode
		}
		return nil
	}

	// TODO: unregister

	for _, g := range b.GroupList {
		err = register(g.Code, g.Name)
		if err != nil {
			return nil, nil, nil, err
		}
	}

	return QQToDiscordMap, discordToQQMap, register, nil
}

func (s *Service) Logger() *logrus.Logger {
	return s.log
}

func (s *Service) Discord() *discordgo.Session {
	return s.discord
}

func (s *Service) QQ() *bot.Bot {
	return s.qq
}
