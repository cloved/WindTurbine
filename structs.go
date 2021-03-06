package main

import "time"

type Setting struct {
	DSN         string
	TablePrefix string
	Listen      string
	Debug       bool
}

type TrackerResource struct {
	setting     Setting
	user_agents []AppTorrentAgent
	credits     map[int]Credit
	log         bool
}

type Credit struct {
	enabled bool
	exp     string
}

/*
 * Structs for BEncode
 */

type Error struct {
	reason string `bencode:"failure reason"`
}

type PeerList struct {
	Interval    int              `bencode:"interval"`
	MinInterval int              `bencode:"min interval"`
	Complete    int              `bencode:"complete"`
	Incomplete  int              `bencode:"incomplete"`
	Peers       []AppTorrentPeer `bencode:"peers"`
}

/*
 * Structs for DB
 */
type AppTorrent struct {
	Id        int `gorm:"AUTO_INCREMENT;primary_key"`
	Tid       int
	InfoHash  string
	Size      int
	Leechers  int
	Seeders   int
	Owner     int
	CreatedAt time.Time
	UpdatedAt time.Time
}

type AppTorrentAgent struct {
	Id            int `gorm:"AUTO_INCREMENT;primary_key"`
	Family        string
	PeerIdPattern string
	AgentPattern  string
	Https         bool `gorm:"DEFAULT:0"`
	Hits          int
}

type AppTorrentHistory struct {
	Id         int `gorm:"AUTO_INCREMENT;primary_key"`
	Uid        int
	TorrentId  int
	Uploaded   int
	Downloaded int
	Left       int
	Leeched    int
	Seeded     int
}

type AppTorrentLog struct {
	Id          int `gorm:"AUTO_INCREMENT;primary_key"`
	Uid         int
	TorrentId   int
	Agent       string
	Passkey     string
	InfoHash    string
	PeerId      string
	Ip          string
	Port        int
	Uploaded    int
	Downloaded  int
	Left        int
	AnnouncedAt time.Time
}

type AppTorrentPeer struct {
	Id          int       `gorm:"AUTO_INCREMENT;primary_key" bencode:"-"`
	Uid         int       `bencode:"-"`
	TorrentId   int       `bencode:"-"`
	Username    string    `bencode:"-"`
	Ip          string    `bencode:"ip"`
	PeerId      string    `bencode:"peer id"`
	Port        int       `bencode:"port"`
	Uploaded    int       `bencode:"-"`
	Downloaded  int       `bencode:"-"`
	Left        int       `bencode:"-"`
	Seeder      bool      `gorm:"DEFAULT:0" bencode:"-"`
	Connectable bool      `gorm:"DEFAULT:0" bencode:"-"`
	Agent       string    `bencode:"-"`
	StartedAt   time.Time `bencode:"-"`
	FinishedAt  time.Time `bencode:"-"`
	LastAction  time.Time `bencode:"-"`
}

type AppTorrentUser struct {
	Uid     int `gorm:"primary_key"`
	Passkey string
}

type BbsThread struct {
	Tid           int `gorm:"AUTO_INCREMENT;primary_key"`
	Disabled      int
	CreatedUserid int
}

type CommonConfig struct {
	Name      string
	Namespace string
	Value     string
}

type User struct {
	Uid      int `gorm:"AUTO_INCREMENT;primary_key"`
	Username string
	Groupid  int
}

type UserBan struct {
	Id     int `gorm:"AUTO_INCREMENT;primary_key"`
	Uid    int
	Reason string
}

type UserData struct {
	Uid     int `gorm:"AUTO_INCREMENT;primary_key"`
	Credit1 float64
	Credit2 float64
	Credit3 float64
	Credit4 float64
	Credit5 float64
	Credit6 float64
	Credit7 float64
	Credit8 float64
}

type WindidUserData UserData
