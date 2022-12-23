package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	qrcodeTerminal "github.com/Baozisoftware/qrcode-terminal-go"
	"github.com/Logiase/MiraiGo-Template/bot"
	"github.com/Logiase/MiraiGo-Template/bot/device"
	"github.com/Logiase/MiraiGo-Template/utils"
	"github.com/Mrs4s/MiraiGo/client"
	"github.com/Mrs4s/MiraiGo/message"
	"github.com/bwmarrin/discordgo"
	"github.com/sirupsen/logrus"
	"github.com/tuotoo/qrcode"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
)

func main() {
	log := logrus.New()
	log.SetFormatter(&logrus.TextFormatter{
		ForceColors: true,
	})

	d, err := discordLogin()
	if err != nil {
		log.Fatalf("failed login discord: %v", err)
	}
	b, err := qqLogin(log)
	if err != nil {
		log.Fatalf("failed login QQ: %v", err)
	}
	h, saveFunc, err := messageHistory(log)
	h.Insert("guildId", "channelId", "discordMsgId", 114514)
	if err != nil {
		log.Fatalf("failed load message history: %v", err)
	}

	b.RefreshList()
	started := false
	log.Infof("Started")

	d.AddHandler(func(s *discordgo.Session, m *discordgo.MessageCreate) {
		if strings.EqualFold(m.Content, "clear") && !started {
			_, err = d.ChannelMessageSend(m.ChannelID, "cleaning")
			if err != nil {
				log.Errorf("error send message: %v", err)
			}
			cleanChannels(s, m)
			_, err = d.ChannelMessageSend(m.ChannelID, "cleaned")
			if err != nil {
				log.Errorf("error send message: %v", err)
			}
			return
		}
		if strings.EqualFold(m.Content, "ping") && !started {
			log.Info("Enabled")
			channelMap, channelIdToGroup, registerChannel, err := initTransport(d, m.GuildID, log, b)
			if err != nil {
				log.Errorf("error init transports: %v", err)
				return
			}

			log.Printf("%v transport inited", len(channelMap))
			syncHistoryMessage(d, m, log, channelMap, h, b)
			d.AddHandler(func(s *discordgo.Session, m *discordgo.MessageCreate) {
				if m.Author.ID == s.State.User.ID {
					return
				}
				g, ok := channelIdToGroup[m.ChannelID]
				if ok {
					HandleDiscordMessage(log, h, m, b, g)
				}
			})
			b.GroupMessageEvent.Subscribe(func(_ *client.QQClient, msg *message.GroupMessage) {
				err = registerChannel(msg.GroupCode, msg.GroupName)
				if err != nil {
					log.Errorf("error register channel %v %v: %v", msg.GroupCode, msg.GroupName, err)
					return
				}
				c, ok := channelMap[msg.GroupCode]
				if ok {
					HandleQQMessage(log, h, msg, d, c, b)
				}
			})
			started = true
			_, err = d.ChannelMessageSend(m.ChannelID, "pong")
			if err != nil {
				log.Errorf("error send message: %v", err)
			}
			/*			buf := ""
						for n, r := range b.GroupList {
							if c, ok := channelMap[r.Code]; ok {
								buf += fmt.Sprintf("%v\n", c.Mention())
								if n%24 == 0 {
									_, err = d.ChannelMessageSend(m.ChannelID, buf)
									if err != nil {
										log.Errorf("error send message: %v", err)
									}
									buf = ""
								}
							}
						}
						_, err = d.ChannelMessageSend(m.ChannelID, buf)
						if err != nil {
							log.Errorf("error send message: %v", err)
						}
			*/
		}
	})

	wait()
	saveFunc()
	b.Release()
}

func syncHistoryMessage(d *discordgo.Session, m *discordgo.MessageCreate, log *logrus.Logger, channelMap map[int64]*discordgo.Channel, h MessageHistorySet, b *bot.Bot) {
	progressMsg, err := d.ChannelMessageSend(m.ChannelID, "syncing history message...")
	if err != nil {
		log.Errorf("error send message: %v", err)
	}
	for groupCode, r := range channelMap {
		lastId, have := h.LastId(r.GuildID, r.ID)
		g, err := b.GetGroupInfo(groupCode)
		if err != nil {
			log.Errorf("error get group info: %v", err)
		}
		if have && g != nil {
			msgs, err := b.GetGroupMessages(groupCode, lastId, g.LastMsgSeq)
			if err != nil {
				log.Errorf("error sync history message: %v", err)
			}
			for i, msg := range msgs {
				HandleQQMessage(log, h, msg, d, r, b)
				_, err := d.ChannelMessageEdit(progressMsg.ChannelID, progressMsg.ID, fmt.Sprintf("syncing history message (%v/%v)", i+1, len(msgs)))
				if err != nil {
					log.Errorf("error sync history message: %v", err)
				}
			}
		}
	}
	_, err = d.ChannelMessageEdit(progressMsg.ChannelID, progressMsg.ID, "synced history message")
	if err != nil {
		log.Errorf("error sync history message: %v", err)
	}
}

type MessageHistory struct {
	QQToDiscord map[int32]string
	DiscordToQQ map[string]int32
	LastId      int64
}

func (h *MessageHistory) Insert(discordMsgId string, qqMsgId int32) {
	h.DiscordToQQ[discordMsgId] = qqMsgId
	h.QQToDiscord[qqMsgId] = discordMsgId
	if h.LastId < int64(qqMsgId) {
		h.LastId = int64(qqMsgId)
	}
}

func (h *MessageHistory) ToQQ(id string) (int32, bool) {
	a, b := h.DiscordToQQ[id]
	return a, b
}

func (h *MessageHistory) ToDiscord(id int32) (string, bool) {
	a, b := h.QQToDiscord[id]
	return a, b
}

type MessageHistorySet map[string]map[string]*MessageHistory

func (s MessageHistorySet) Save(w io.Writer) error {
	return json.NewEncoder(w).Encode(s)
}

func ReadMessageHistorySet(r io.Reader) (MessageHistorySet, error) {
	s := make(MessageHistorySet)
	return s, json.NewDecoder(r).Decode(&s)
}

func (s MessageHistorySet) ToQQ(guildId, channelId, msgId string) (int32, bool) {
	m, ok := s[guildId]
	if !ok {
		return 0, false
	}
	h, ok := m[channelId]
	if !ok {
		return 0, false
	}
	return h.ToQQ(msgId)
}

func (s MessageHistorySet) ToDiscord(guildId, channelId string, msgId int32) (string, bool) {
	m, ok := s[guildId]
	if !ok {
		return "", false
	}
	h, ok := m[channelId]
	if !ok {
		return "", false
	}
	return h.ToDiscord(msgId)
}

func (s MessageHistorySet) lazy(guildId string, channelId string) {
	if _, ok := s[guildId]; !ok {
		s[guildId] = make(map[string]*MessageHistory)
	}
	if _, ok := s[guildId][channelId]; !ok {
		s[guildId][channelId] = &MessageHistory{
			QQToDiscord: make(map[int32]string),
			DiscordToQQ: make(map[string]int32),
		}
	}
}

func (s MessageHistorySet) Insert(guildId, channelId, discordMsgId string, qqMsgId int32) {
	s.lazy(guildId, channelId)
	s[guildId][channelId].Insert(discordMsgId, qqMsgId)
}

func (s MessageHistorySet) LastId(guildId, channelId string) (int64, bool) {
	s.lazy(guildId, channelId)
	id := s[guildId][channelId].LastId
	return id, id != 0
}

func cleanChannels(s *discordgo.Session, m *discordgo.MessageCreate) {
	c, _ := s.GuildChannels(m.GuildID)
	for _, cc := range c {
		if len(strings.Split(cc.Name, "_")) >= 2 {
			_, _ = s.ChannelDelete(cc.ID)
		}
	}
}

func HandleDiscordMessage(log *logrus.Logger, h MessageHistorySet, m *discordgo.MessageCreate, b *bot.Bot, groupCode int64) {
	msg := m.ContentWithMentionsReplaced()
	qqMsg := message.NewSendingMessage()
	if m.Type == discordgo.MessageTypeReply {
		seq, has := h.ToQQ(m.MessageReference.GuildID, m.MessageReference.ChannelID, m.MessageReference.MessageID)
		if has {
			msgs, err := b.GetGroupMessages(groupCode, int64(seq), int64(seq))
			if err != nil {
				log.Errorf("error get group message: %v", msgs)
			} else {
				qqMsg.Append(message.NewReply(msgs[0]))
			}
		}
	}
	if len(msg) != 0 {
		qqMsg.Append(message.NewText(msg))
	}

	for _, a := range m.Embeds {
		switch a.Type {
		case discordgo.EmbedTypeImage, discordgo.EmbedTypeGifv:
		//TODO
		case discordgo.EmbedTypeLink:
			qqMsg.Append(message.NewUrlShare(a.URL, a.Title, a.Description, ""))
		case discordgo.EmbedTypeVideo:
			//TODO
		}
	}
	for _, a := range m.Attachments {
		body, err := urlGet(a.ProxyURL)
		if err != nil {
			log.Errorf("error get url: %v", body)
		}
		bb, err := io.ReadAll(body)
		_ = body.Close()
		if err != nil {
			log.Errorf("error copy: %v", err)
			continue
		}
		switch strings.ToLower(strings.SplitN(a.ContentType, "/", 2)[0]) {
		case "image":
			retry := 255
			for retry > 0 {
				img, err := b.UploadGroupImage(groupCode, bytes.NewReader(bb))
				if err != nil {
					retry--
					continue
				}
				qqMsg.Elements = append(qqMsg.Elements, img)
				break
			}
		default:
			err := b.UploadFile(message.Source{
				SourceType: message.SourceGroup,
				PrimaryID:  groupCode,
			}, &client.LocalFile{
				FileName: a.Filename,
				Body:     bytes.NewReader(bb),
			})
			if err != nil {
				log.Errorf("error uploading file %v: %v", a.Filename, err)
				continue
			}
		}
	}
	grpMsg := b.SendGroupMessage(groupCode, qqMsg)
	h.Insert(m.GuildID, m.ChannelID, m.ID, grpMsg.Id)
}

func HandleQQMessage(log *logrus.Logger, h MessageHistorySet, msg *message.GroupMessage, d *discordgo.Session, c *discordgo.Channel, b *bot.Bot) {
	m := &discordgo.MessageSend{}

	for _, e := range msg.Elements {
		switch i := e.(type) {
		case *message.ReplyElement:
			d, ok := h.ToDiscord(c.GuildID, c.ID, i.ReplySeq)
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
	m.Content = fmt.Sprintf("%v(%v): %v", msg.Sender.Nickname, msg.Sender.Uin, QQMsgToString(msg, b, c))
	discordMsg, err := d.ChannelMessageSendComplex(c.ID, m)
	h.Insert(c.GuildID, c.ID, discordMsg.ID, msg.Id)
	if err != nil {
		log.Errorf("error sending message: %v", err)
	}
	for _, e := range msg.Elements {
		switch i := e.(type) {
		case *message.ShortVideoElement:
			r, err := urlGet(b.GetShortVideoUrl(i.Uuid, i.Md5))
			if err != nil {
				log.Errorf("error get url: %v", err)
			}
			_, err = d.ChannelFileSend(c.ID, filepath.Base(i.Name), r)
			if err != nil {
				log.Errorf("error uploading video file: %v", err)
			}
			_ = r.Close()
		}
	}
}

func QQMsgToString(msg *message.GroupMessage, b *bot.Bot, c *discordgo.Channel) (res string) {
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

func urlGet(url string) (io.ReadCloser, error) {
	retry := 255
	var lastErr error
	for retry > 0 {
		req, err := http.Get(url)
		if err != nil {
			lastErr = err
			retry--
			continue
		}
		return req.Body, err
	}
	return nil, lastErr
}

func initTransport(d *discordgo.Session, guildId string, log *logrus.Logger, b *bot.Bot) (map[int64]*discordgo.Channel, map[string]int64, func(groupCode int64, groupName string) error, error) {
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
				log.Debugf("reuse: %v", parts)
				channelMap[g.Code] = c
				channelIdToGroup[c.ID] = g.Code
				break
			}
		}
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

func discordLogin() (*discordgo.Session, error) {
	d, _ := discordgo.New("Bot MTA0NzM4NzEwMjk4NTMyMjQ5Ng.Gw5aa9.yz5O9QY8H40JZsjx4izWuV-SjhAmQaqglU60-w")
	return d, d.Open()
}

func wait() {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt, os.Kill)
	<-ch
}

type LoginData struct {
	Account  int64  `json:"account"`
	Method   string `json:"login-method"`
	Password string `json:"password"`
}

func qqLogin(log *logrus.Logger) (*bot.Bot, error) {
	fileExists := func(file string) bool {
		_, err := os.Stat(file)
		if os.IsNotExist(err) {
			return false
		}
		if err != nil {
			return false
		}
		return true
	}
	if !fileExists("./device.json") {
		w, err := os.Create("./device.json")
		if err != nil {
			return nil, err
		}
		defer w.Close()
		err = device.GenRandomDeviceWriter(w)
		if err != nil {
			return nil, err
		}
	}
	deviceJson, err := utils.ReadFile("./device.json")
	if err != nil {
		return nil, err
	}
	if !fileExists("./config.json") {
		w, err := os.Create("./config.json")
		if err != nil {
			return nil, err
		}
		defer w.Close()
		err = json.NewEncoder(w).Encode(LoginData{
			Account:  123456,
			Method:   bot.LoginMethodCommon,
			Password: "123456",
		})
		if err != nil {
			return nil, err
		}
	}

	loginJson, err := utils.ReadFile("./config.json")
	if err != nil {
		return nil, err
	}

	loginData := LoginData{}

	err = json.Unmarshal(loginJson, &loginData)
	if err != nil {
		return nil, err
	}
	lg := &bot.Loginer{
		Log:               log,
		Protocol:          device.AndroidWatch,
		Method:            bot.LoginMethod(loginData.Method),
		DeviceJSONContent: deviceJson,
		Account:           loginData.Account,
		Password:          loginData.Password,
		ReadTokenCache: func() ([]byte, bool) {
			c, err := utils.ReadFile("./session.dat")
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
			log.Errorf(sc.Err().Error())
			return ""
		},
		HandleCaptcha: func(i []byte) string {
			panic("TODO: HandleCaptcha")
		},
	}
	// 登录
	b, err := lg.Login()
	if err != nil {
		return nil, err
	}
	log.Printf("登陆成功")
	err = os.WriteFile("./session.dat", b.GenToken(), 0666)
	return b, err
}

func messageHistory(log *logrus.Logger) (MessageHistorySet, func(), error) {
	fileExists := func(file string) bool {
		_, err := os.Stat(file)
		if os.IsNotExist(err) {
			return false
		}
		if err != nil {
			return false
		}
		return true
	}
	if !fileExists("./message.dat") {
		f, _ := os.Create("./message.dat")
		_ = f.Close()
	}
	f, err := os.Open("./message.dat")
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()
	h, err := ReadMessageHistorySet(f)
	if err != nil && err != io.EOF {
		return nil, nil, err
	}
	return h, func() {
		f2, err := os.OpenFile("./message.dat", os.O_WRONLY|os.O_TRUNC, 0666)
		if err != nil {
			log.Fatalf("error opening ./message.dat: %v", err)
		}
		err = h.Save(f2)
		if err != nil {
			log.Fatalf("error saving message history: %v", err)
		}
		log.Info("message history saved")
		_ = f2.Close()
	}, nil
}
