package backend

import (
	"bytes"
	"fmt"
	"github.com/Blackjack200/Qiscord/util"
	"github.com/Mrs4s/MiraiGo/client"
	"github.com/Mrs4s/MiraiGo/message"
	"github.com/bwmarrin/discordgo"
	"io"
	"path/filepath"
	"strings"
)

var translateMessagePanicHandler = func(s *Service, f string) func() {
	return func() {
		panicThing := recover()
		if panicThing != nil {
			s.log.Errorf("panic: translate %v msg: %v", s, panicThing)
		}
	}
}

func (s *Service) HandleDiscordMessage(group *client.GroupInfo, discordMsg *discordgo.MessageCreate) {
	defer translateMessagePanicHandler(s, "discord")
	discordMsgFormatted := discordMsg.ContentWithMentionsReplaced()
	translated := message.NewSendingMessage()

	if discordMsg.Type != discordgo.MessageTypeReply && discordMsg.Type != discordgo.MessageTypeDefault {
		return
	}

	if discordMsg.Type == discordgo.MessageTypeReply {
		seq, has := s.history.ToQQ(discordMsg.MessageReference.GuildID, discordMsg.MessageReference.ChannelID, discordMsg.MessageReference.MessageID)
		if has {
			// we don't care this error
			msgs, _ := s.qq.GetGroupMessages(group.Code, int64(seq), int64(seq))
			if len(msgs) > 0 {
				for _, msg := range msgs {
					translated.Append(message.NewReply(msg))
				}
			}
		}
	}

	if len(discordMsgFormatted) != 0 {
		translated.Append(message.NewText(discordMsgFormatted))
	}

	//we don't need to translate embeds

	for _, a := range discordMsg.Attachments {
		r := util.MustNotNil[io.ReadCloser](util.UrlGet(a.ProxyURL))
		body := util.MustAnyByteSlice(io.ReadAll(r))
		util.Must(r.Close())
		switch strings.ToLower(strings.SplitN(a.ContentType, "/", 2)[0]) {
		case "image":
			retry := 255
			for retry > 0 {
				//goland:noinspection GoDeprecation
				img, err := s.qq.UploadGroupImage(group.Code, bytes.NewReader(body))
				if err != nil {
					retry--
					continue
				}
				translated.Elements = append(translated.Elements, img)
				break
			}
		default:
			util.Must(s.qq.UploadFile(message.Source{
				SourceType: message.SourceGroup,
				PrimaryID:  group.Code,
			}, &client.LocalFile{
				FileName: a.Filename,
				Body:     bytes.NewReader(body),
			}))
		}
	}
	grpMsg := s.qq.SendGroupMessage(group.Code, translated)
	s.history.Insert(discordMsg.GuildID, discordMsg.ChannelID, discordMsg.ID, grpMsg.Id)
}

func (s *Service) handleQQMessage(c *discordgo.Channel, msg *message.GroupMessage) {
	defer translateMessagePanicHandler(s, "qq")
	m := &discordgo.MessageSend{}
	isReply := false
	for _, e := range msg.Elements {
		switch i := e.(type) {
		case *message.ReplyElement:
			isReply = true
			d, ok := s.history.ToDiscord(c.GuildID, c.ID, i.ReplySeq)
			if ok {
				m.Reference = &discordgo.MessageReference{
					MessageID: d,
					ChannelID: c.ID,
					GuildID:   c.GuildID,
				}
			}
		case *message.GroupImageElement:
			url := i.Url
			m.Embeds = append(m.Embeds, &discordgo.MessageEmbed{
				Image: &discordgo.MessageEmbedImage{
					URL:      url,
					ProxyURL: url,
					Width:    int(i.Width),
					Height:   int(i.Height),
				},
			})
		}
	}
	m.Content = fmt.Sprintf("%v(%v): %v", msg.Sender.DisplayName(), msg.Sender.Uin, s.QQMsgToString(msg, isReply))

	discordMsg := util.MustNotNil[*discordgo.Message](s.discord.ChannelMessageSendComplex(c.ID, m))
	s.history.Insert(c.GuildID, c.ID, discordMsg.ID, msg.Id)

	// slow download speed, so do this in other goroutine
	for _, e := range msg.Elements {
		switch i := e.(type) {
		case *message.ShortVideoElement:
			go func() {
				defer func() {
					panicThing := recover()
					if panicThing != nil {
						s.log.Errorf("panic: get video: %v", panicThing)
					}
				}()
				r := util.MustNotNil[io.ReadCloser](util.UrlGet(s.qq.GetShortVideoUrl(i.Uuid, i.Md5)))
				fileMsg := &discordgo.MessageSend{
					Reference: discordMsg.Reference(),
					Files: []*discordgo.File{{
						Name:   filepath.Base(i.Name),
						Reader: r,
					}},
				}
				util.Must(s.discord.ChannelMessageSendComplex(c.ID, fileMsg))
				util.Must(r.Close())
			}()
		}
	}
}

func (s *Service) QQMsgToString(msg *message.GroupMessage, isReply bool) (res string) {
	for _, elem := range msg.Elements {
		switch e := elem.(type) {
		case *message.TextElement:
			res += e.Content
		case *message.FaceElement:
			res += fmt.Sprintf("[%v]", e.Name)
		case *message.MarketFaceElement:
			res += fmt.Sprintf("[%v]", e.Name)
		case *message.AtElement:
			if e.Target == s.qq.Uin {
				if !isReply {
					res += "@here"
				}
			} else {
				res += e.Display
			}
		case *message.RedBagElement:
			res += fmt.Sprintf("[RedBag: %v]", e.Title)
		case *message.ShortVideoElement:
			res += fmt.Sprintf("[Video: %v]", filepath.Base(e.Name))
		case *message.GroupFileElement:
			res += fmt.Sprintf("[File: %v: %v]", e.Name, s.qq.GetGroupFileUrl(msg.GroupCode, e.Path, e.Busid))
		}
	}
	return
}
