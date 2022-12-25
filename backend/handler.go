package backend

import (
	"fmt"
	"github.com/Blackjack200/Qiscord/util"
	"github.com/Mrs4s/MiraiGo/client"
	"github.com/Mrs4s/MiraiGo/message"
	"github.com/bwmarrin/discordgo"
)

func (s *Service) initHandler() {
	s.startRecallHandler()
	s.startMessageHandler()
}

func (s *Service) startMessageHandler() {
	s.discord.AddHandler(func(_ *discordgo.Session, discordMsg *discordgo.MessageCreate) {
		if discordMsg.Author.ID == s.discord.State.User.ID {
			return
		}
		if group, ok := s.channelToGroup(discordMsg.ChannelID); ok {
			s.HandleDiscordMessage(group, discordMsg)
		}
	})
	s.qq.GroupMessageEvent.Subscribe(func(_ *client.QQClient, qqMsg *message.GroupMessage) {
		util.Must(s.registerFunc(qqMsg.GroupCode, qqMsg.GroupName))
		if channel, ok := s.groupToChannel(qqMsg.GroupCode); ok {
			s.handleQQMessage(channel, qqMsg, true)
		}
	})
	s.qq.GroupJoinEvent.Subscribe(func(_ *client.QQClient, group *client.GroupInfo) {
		util.Must(s.registerFunc(group.Code, group.Name))
	})
}

func (s *Service) startRecallHandler() {
	recallPanicRecover := func(f string) func() {
		return func() {
			panicThing := recover()
			if panicThing != nil {
				s.log.Errorf("recalling %v message: %v", f, panicThing)
			}
		}
	}
	s.discord.AddHandler(func(_ *discordgo.Session, deletedMsg *discordgo.MessageDelete) {
		defer recallPanicRecover("discord")()
		group, ok := s.channelToGroup(deletedMsg.ChannelID)
		if ok {
			qqMsgId, ok := s.history.ToQQ(deletedMsg.GuildID, deletedMsg.ChannelID, deletedMsg.ID)
			if ok {
				msg, _ := s.qq.GetGroupMessages(group.Code, int64(qqMsgId), int64(qqMsgId))
				if len(msg) > 0 {
					util.Must(s.qq.RecallGroupMessage(group.Code, msg[0].Id, msg[0].InternalId))
				}
			}
		}
	})
	s.qq.GroupMessageRecalledEvent.Subscribe(func(_ *client.QQClient, e *client.GroupMessageRecalledEvent) {
		defer recallPanicRecover("qq")()
		channel, ok := s.groupToChannel(e.GroupCode)
		if ok {
			discordMsgId, ok := s.history.ToDiscord(channel.GuildID, channel.ID, e.MessageId)
			if ok {
				info := util.MustNotNil[*client.GroupInfo](s.qq.FindGroup(e.GroupCode))
				operator := info.FindMemberWithoutLock(e.OperatorUin)
				author := info.FindMemberWithoutLock(e.AuthorUin)
				util.MustBool(operator != nil, author != nil)
				util.Must(s.discord.ChannelMessageEdit(
					channel.ID, discordMsgId,
					fmt.Sprintf(
						"%v Recalled %v's message",
						operator.DisplayName(), author.DisplayName(),
					),
				))
			}
		}
	})
}
