package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	qrcodeTerminal "github.com/Baozisoftware/qrcode-terminal-go"
	"github.com/Blackjack200/Qiscord/storage"
	"github.com/Blackjack200/Qiscord/util"
	"github.com/Logiase/MiraiGo-Template/bot"
	"github.com/Logiase/MiraiGo-Template/bot/device"
	"github.com/Mrs4s/MiraiGo/client"
	"github.com/Mrs4s/MiraiGo/message"
	"github.com/bwmarrin/discordgo"
	"github.com/sirupsen/logrus"
	"github.com/tuotoo/qrcode"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
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
		log.Fatalf("error read config: %v", err)
	}

	d, err := discordLogin(cfg)
	if err != nil {
		log.Fatalf("failed login discord: %v", err)
	}

	b, err := qqLogin(log, cfg)
	if err != nil {
		log.Fatalf("failed login QQ: %v", err)
	}

	h, saveFunc, err := messageHistory(log)
	if err != nil {
		log.Fatalf("failed load message history: %v", err)
	}

	b.RefreshList()
	started := false
	log.Infof("Started")

	d.AddHandler(func(s *discordgo.Session, discordMsg *discordgo.MessageCreate) {
		if strings.EqualFold(discordMsg.Content, "clear") && !started {
			mainProgressFunc, err := d.ChannelMessageSend(discordMsg.ChannelID, "cleaning")
			util.Optional(err)

			cleanChannels(s, discordMsg)

			_, err = d.ChannelMessageEdit(discordMsg.ChannelID, mainProgressFunc.ID, "cleaned")
			util.Optional(err)
			return
		}

		if strings.EqualFold(discordMsg.Content, "ping") && !started {
			log.Info("Enabled")

			channelMap, channelIdToGroup, registerChannel, err := initTransport(d, b, discordMsg.GuildID)
			d := &Data{
				Log:        log,
				Discord:    d,
				QQ:         b,
				History:    h,
				ChannelMap: channelMap,
			}

			if err != nil {
				log.Errorf("init transports: %v", err)
				return
			}

			log.Printf("%v transport inited", len(channelMap))

			log.Printf("syncing history messages")
			syncHistoryMessage(d, discordMsg.ChannelID)
			log.Printf("synced history messages")

			d.Discord.AddHandler(func(s *discordgo.Session, deletedMsg *discordgo.MessageDelete) {
				groupCode, ok := channelIdToGroup[deletedMsg.ChannelID]
				if ok {
					qqMsgId, ok := d.History.ToQQ(deletedMsg.GuildID, deletedMsg.ChannelID, deletedMsg.ID)
					if ok {
						msg, _ := d.QQ.GetGroupMessages(groupCode, int64(qqMsgId), int64(qqMsgId))
						if len(msg) > 0 {
							util.Must(d.QQ.RecallGroupMessage(groupCode, msg[0].Id, msg[0].InternalId))
						}
					}
				}
			})
			d.Discord.AddHandler(func(s *discordgo.Session, discordMsg *discordgo.MessageCreate) {
				if discordMsg.Author.ID == s.State.User.ID {
					return
				}
				groupCode, ok := channelIdToGroup[discordMsg.ChannelID]
				group := d.QQ.FindGroup(groupCode)
				if ok {
					HandleDiscordMessage(d, group, discordMsg)
				}
			})
			d.QQ.GroupMessageRecalledEvent.Subscribe(func(_ *client.QQClient, e *client.GroupMessageRecalledEvent) {
				channel, ok := channelMap[e.GroupCode]
				if ok {
					discordMsgId, ok := d.History.ToDiscord(channel.GuildID, channel.ID, e.MessageId)
					if ok {
						info := util.MustNotNil[*client.GroupInfo](d.QQ.FindGroup(e.GroupCode))
						operator := info.FindMemberWithoutLock(e.OperatorUin)
						author := info.FindMemberWithoutLock(e.AuthorUin)
						util.MustBool(operator != nil, author != nil)
						util.Optional(d.Discord.ChannelMessageEdit(
							channel.ID, discordMsgId,
							fmt.Sprintf(
								"%v Recalled %v's message",
								operator.DisplayName(), author.DisplayName(),
							),
						))
					}
				}
			})
			d.QQ.GroupMessageEvent.Subscribe(func(_ *client.QQClient, qqMsg *message.GroupMessage) {
				err = registerChannel(qqMsg.GroupCode, qqMsg.GroupName)
				if err != nil {
					log.Errorf("error register channel %v %v: %v", qqMsg.GroupCode, qqMsg.GroupName, err)
					return
				}
				c, ok := channelMap[qqMsg.GroupCode]
				if ok {
					HandleQQMessage(d, c, qqMsg)
				}
			})

			started = true
			util.Optional(d.Discord.ChannelMessageSend(discordMsg.ChannelID, "pong"))
		}
	})

	wait()
	saveFunc()
	b.Release()
}

func cleanChannels(s *discordgo.Session, m *discordgo.MessageCreate) {
	c, _ := s.GuildChannels(m.GuildID)
	for _, cc := range c {
		if len(strings.Split(cc.Name, "_")) >= 2 {
			_, _ = s.ChannelDelete(cc.ID)
		}
	}
}

// dumpTransports for debug uses
func dumpTransports(d *Data, channelId string) {
	buf := ""
	for n, r := range d.QQ.GroupList {
		if c, ok := d.ChannelMap[r.Code]; ok {
			buf += fmt.Sprintf("%v\n", c.Mention())
			if n%24 == 0 {
				util.Optional(d.Discord.ChannelMessageSend(channelId, buf))
				buf = ""
			}
		}
	}
	util.Optional(d.Discord.ChannelMessageSend(channelId, buf))
}

type Data struct {
	Log        *logrus.Logger
	Discord    *discordgo.Session
	QQ         *bot.Bot
	History    storage.MessageHistory
	ChannelMap map[int64]*discordgo.Channel
}

func syncHistoryMessage(d *Data, channelId string) {
	defer func() {
		panicThing := recover()
		if panicThing != nil {
			d.Log.Errorf("panic: sync history message: %v", panicThing)
		}
	}()
	wg := &sync.WaitGroup{}

	mainProgressMsg, err := d.Discord.ChannelMessageSend(channelId, "syncing history message...")
	util.Must(err)

	for groupCode, channel := range d.ChannelMap {
		//confusing :p
		groupCode := groupCode
		channel := channel

		wg.Add(1)
		go func() {
			defer func() {
				panicThing := recover()
				if panicThing != nil {
					d.Log.Errorf("panic: sync history message: %v", panicThing)
				}
			}()
			defer wg.Done()
			lastId, ok := d.History.LastId(channel.GuildID, channel.ID)
			groupInfo, err := d.QQ.GetGroupInfo(groupCode)
			util.Must(err)
			if ok && groupInfo != nil {
				//we don't care this error
				msgs, _ := d.QQ.GetGroupMessages(groupCode, lastId+1, groupInfo.LastMsgSeq)
				if len(msgs) > 0 {
					subProgressMsg := util.MustNotNil[*discordgo.Message](d.Discord.ChannelMessageSend(
						channelId,
						fmt.Sprintf(" - Syncing %v", groupInfo.Name),
					))

					for i, msg := range msgs {
						HandleQQMessage(d, channel, msg)
						util.Must(d.Discord.ChannelMessageEdit(
							subProgressMsg.ChannelID, mainProgressMsg.ID,
							fmt.Sprintf(" - Syncing %v (%v/%v)", groupInfo.Name, i+1, len(msgs)),
						))
					}
					util.Must(d.Discord.ChannelMessageDelete(subProgressMsg.ChannelID, subProgressMsg.ID))
				}
			}
		}()

	}
	wg.Wait()
	util.Must(d.Discord.ChannelMessageEdit(mainProgressMsg.ChannelID, mainProgressMsg.ID, "synced history message"))
}

func HandleDiscordMessage(d *Data, group *client.GroupInfo, discordMsg *discordgo.MessageCreate) {
	defer func() {
		panicThing := recover()
		if panicThing != nil {
			d.Log.Errorf("panic: translate discord msg: %v", panicThing)
		}
	}()
	discordMsgFormatted := discordMsg.ContentWithMentionsReplaced()
	qqMsg := message.NewSendingMessage()

	if discordMsg.Type == discordgo.MessageTypeReply {
		seq, has := d.History.ToQQ(discordMsg.MessageReference.GuildID, discordMsg.MessageReference.ChannelID, discordMsg.MessageReference.MessageID)
		if has {
			// we don't care this error
			msgs, _ := d.QQ.GetGroupMessages(group.Code, int64(seq), int64(seq))
			if len(msgs) == 1 {
				qqMsg.Append(message.NewReply(msgs[0]))
			}
		}
	}

	if discordMsg.Type == discordgo.MessageTypeDefault && len(discordMsgFormatted) != 0 {
		qqMsg.Append(message.NewText(discordMsgFormatted))
	}

	// embeds is not necessary
	/*
		for _, a := range discordMsg.Embeds {
			switch a.Type {
			case discordgo.EmbedTypeImage, discordgo.EmbedTypeGifv:
			case discordgo.EmbedTypeLink:
				qqMsg.Append(message.NewUrlShare(a.URL, a.Title, a.Description, ""))
			case discordgo.EmbedTypeVideo:
			}
		}
	*/

	for _, a := range discordMsg.Attachments {
		r := util.MustNotNil[io.ReadCloser](util.UrlGet(a.ProxyURL))
		body := util.MustAnyByteSlice(io.ReadAll(r))
		util.Must(r.Close())
		switch strings.ToLower(strings.SplitN(a.ContentType, "/", 2)[0]) {
		case "image":
			retry := 255
			for retry > 0 {
				img, err := d.QQ.UploadGroupImage(group.Code, bytes.NewReader(body))
				if err != nil {
					retry--
					continue
				}
				qqMsg.Elements = append(qqMsg.Elements, img)
				break
			}
		default:
			util.Must(d.QQ.UploadFile(message.Source{
				SourceType: message.SourceGroup,
				PrimaryID:  group.Code,
			}, &client.LocalFile{
				FileName: a.Filename,
				Body:     bytes.NewReader(body),
			}))
		}
	}
	grpMsg := d.QQ.SendGroupMessage(group.Code, qqMsg)
	d.History.Insert(discordMsg.GuildID, discordMsg.ChannelID, discordMsg.ID, grpMsg.Id)
}

func HandleQQMessage(d *Data, c *discordgo.Channel, msg *message.GroupMessage) {
	defer func() {
		panicThing := recover()
		if panicThing != nil {
			d.Log.Errorf("panic: translate qq msg: %v", panicThing)
		}
	}()
	m := &discordgo.MessageSend{}
	for _, e := range msg.Elements {
		switch i := e.(type) {
		case *message.ReplyElement:
			d, ok := d.History.ToDiscord(c.GuildID, c.ID, i.ReplySeq)
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
	m.Content = fmt.Sprintf("%v(%v): %v", msg.Sender.DisplayName(), msg.Sender.Uin, QQMsgToString(msg, d.QQ))
	discordMsg := util.MustNotNil[*discordgo.Message](d.Discord.ChannelMessageSendComplex(c.ID, m))
	d.History.Insert(c.GuildID, c.ID, discordMsg.ID, msg.Id)

	for _, e := range msg.Elements {
		switch i := e.(type) {
		case *message.ShortVideoElement:
			r := util.MustNotNil[io.ReadCloser](util.UrlGet(d.QQ.GetShortVideoUrl(i.Uuid, i.Md5)))
			util.Must(d.Discord.ChannelFileSend(c.ID, filepath.Base(i.Name), r))
			util.Must(r.Close())
		}
	}
}

func QQMsgToString(msg *message.GroupMessage, b *bot.Bot) (res string) {
	for _, elem := range msg.Elements {
		switch e := elem.(type) {
		case *message.TextElement:
			res += e.Content
		case *message.FaceElement:
			res += "[" + e.Name + "]"
		case *message.MarketFaceElement:
			res += "[" + e.Name + "]"
		case *message.AtElement:
			if e.Target == b.Uin {
				res += "@here"
			} else {
				res += e.Display
			}
		case *message.RedBagElement:
			res += "[RedBag:" + e.Title + "]"
		case *message.ShortVideoElement:
			res += "[Video: +" + filepath.Base(e.Name) + "]"
		case *message.GroupFileElement:
			res += "[File:" + e.Name + " " + b.GetGroupFileUrl(msg.GroupCode, e.Path, e.Busid) + "]"
		}
	}
	return
}

func initTransport(d *discordgo.Session, b *bot.Bot, guildId string) (map[int64]*discordgo.Channel, map[string]int64, func(groupCode int64, groupName string) error, error) {
	cs, err := d.GuildChannels(guildId)
	if err != nil {
		return nil, nil, nil, err
	}

	channelMap := make(map[int64]*discordgo.Channel)
	channelIdToGroup := make(map[string]int64)

	for _, c := range cs {
		splits := strings.Split(c.Name, "_")
		parts := splits[len(splits)-1]
		for _, g := range b.GroupList {
			if parts == strconv.FormatInt(g.Code, 10) {
				channelMap[g.Code] = c
				channelIdToGroup[c.ID] = g.Code
				break
			}
		}
		// TODO delete quited groups
	}

	register := func(groupCode int64, groupName string) error {
		if _, ok := channelMap[groupCode]; !ok {
			c, err := d.GuildChannelCreateComplex(guildId, discordgo.GuildChannelCreateData{
				Name:  fmt.Sprintf("%v_%v", groupName, groupCode),
				Type:  discordgo.ChannelTypeGuildText,
				Topic: "QQ",
			})
			if err != nil {
				return err
			}
			channelMap[groupCode] = c
			channelIdToGroup[c.ID] = groupCode
		}
		return nil
	}

	for _, g := range b.GroupList {
		err = register(g.Code, g.Name)
		if err != nil {
			return nil, nil, nil, err
		}
	}

	return channelMap, channelIdToGroup, register, nil
}

func wait() {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt, os.Kill)
	<-ch
}

type Config struct {
	DiscordToken string `json:"discord-token"`
	Account      int64  `json:"account"`
	Method       string `json:"login-method"`
	Password     string `json:"password"`
}

func readConfig() (Config, error) {
	configJson := util.MustReadFile("./config.json")
	config := Config{}
	return config, json.Unmarshal(configJson, &config)
}

func messageHistory(log *logrus.Logger) (storage.MessageHistory, func(), error) {
	r := util.MustInitFile("./message.dat")

	h, err := storage.ReadMessageHistory(r)
	util.Must(r.Close())

	if err != nil && err != io.EOF {
		return nil, nil, err
	}

	return h, func() {
		f := util.MustOpenFile("./message.dat")
		util.Must(h.Save(f), f.Close())
		log.Info("message history saved")
	}, nil
}

func discordLogin(cfg Config) (*discordgo.Session, error) {
	d, _ := discordgo.New("Bot " + cfg.DiscordToken)
	return d, d.Open()
}

func qqLogin(log *logrus.Logger, cfg Config) (*bot.Bot, error) {
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
