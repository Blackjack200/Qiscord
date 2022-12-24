package storage

import (
	"encoding/json"
	"io"
	"sync"
)

type messageHistoryEntry struct {
	mu          *sync.Mutex `json:"-"`
	QQToDiscord map[int32]string
	DiscordToQQ map[string]int32
	LastId      int64
}

func (h *messageHistoryEntry) Insert(discordMsgId string, qqMsgId int32) {
	if h.mu == nil {
		h.mu = &sync.Mutex{}
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.DiscordToQQ[discordMsgId] = qqMsgId
	h.QQToDiscord[qqMsgId] = discordMsgId
	if h.LastId < int64(qqMsgId) {
		h.LastId = int64(qqMsgId)
	}
}

func (h *messageHistoryEntry) ToQQ(id string) (int32, bool) {
	a, b := h.DiscordToQQ[id]
	return a, b
}

func (h *messageHistoryEntry) ToDiscord(id int32) (string, bool) {
	a, b := h.QQToDiscord[id]
	return a, b
}

func NewMessageHistory() MessageHistory {
	return make(MessageHistory)
}

var mu = &sync.Mutex{}

type MessageHistory map[string]map[string]*messageHistoryEntry

func (s MessageHistory) Save(w io.Writer) error {
	return json.NewEncoder(w).Encode(s)
}

func ReadMessageHistory(r io.Reader) (MessageHistory, error) {
	s := NewMessageHistory()
	return s, json.NewDecoder(r).Decode(&s)
}

func (s MessageHistory) ToQQ(guildId, channelId, msgId string) (int32, bool) {
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

func (s MessageHistory) ToDiscord(guildId, channelId string, msgId int32) (string, bool) {
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

func (s MessageHistory) lazy(guildId string, channelId string) {
	mu.Lock()
	defer mu.Unlock()
	if _, ok := s[guildId]; !ok {
		s[guildId] = make(map[string]*messageHistoryEntry)
	}
	if _, ok := s[guildId][channelId]; !ok {
		s[guildId][channelId] = &messageHistoryEntry{
			QQToDiscord: make(map[int32]string),
			DiscordToQQ: make(map[string]int32),
		}
	}
}

func (s MessageHistory) Insert(guildId, channelId, discordMsgId string, qqMsgId int32) {
	s.lazy(guildId, channelId)
	s[guildId][channelId].Insert(discordMsgId, qqMsgId)
}

func (s MessageHistory) LastId(guildId, channelId string) (int64, bool) {
	s.lazy(guildId, channelId)
	id := s[guildId][channelId].LastId
	return id, id != 0
}
