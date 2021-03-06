package main

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"github.com/Knetic/govaluate"
	"github.com/jackpal/bencode-go"
	"github.com/jinzhu/gorm"
	_ "github.com/jinzhu/gorm/dialects/mysql"
	"github.com/kataras/iris"
	"github.com/kinosang/php_serialize"
	"github.com/oleiade/reflections"
	"io/ioutil"
	"math"
	"net"
	"os"
	"regexp"
	"strings"
	"time"
)

func main() {
	// Read config file
	xmlFile, err := os.Open("config.xml")

	if err != nil {
		panic(err)
	}

	defer xmlFile.Close()

	b, _ := ioutil.ReadAll(xmlFile)

	var s Setting
	xml.Unmarshal(b, &s)

	// Initialize GORM
	gorm.DefaultTableNameHandler = func(db *gorm.DB, defaultTableName string) string {
		switch defaultTableName {
		case "common_configs":
			defaultTableName = "common_config"
		case "user_bans":
			defaultTableName = "user_ban"
		case "user_datas":
			defaultTableName = "user_data"
		case "users":
			defaultTableName = "user"
		case "windid_user_datas":
			defaultTableName = "windid_user_data"
		}
		return s.TablePrefix + defaultTableName
	}

	db, err := gorm.Open("mysql", s.DSN)
	db.LogMode(s.Debug)

	if err != nil {
		panic(err)
	}

	defer db.Close()

	// Load User-Agent whitelist
	var user_agents []AppTorrentAgent
	db.Order("id").Find(&user_agents)

	// Load credits expression
	var common_config CommonConfig
	db.Where("name = \"app.torrent.credits\"").First(&common_config)

	decoder := php_serialize.NewUnSerializer(common_config.Value)
	exp_pvalue, err := decoder.Decode()

	if err != nil {
		panic(err)
	}

	exp_array, _ := exp_pvalue.(php_serialize.PhpArray)
	credits := make(map[int]Credit)

	for k, v := range exp_array {
		v_array := v.(php_serialize.PhpArray)
		v_enabled := v_array["enabled"].(string)
		v_exp := v_array["exp"].(string)

		credits[k.(int)] = Credit{enabled: v_enabled == "1", exp: v_exp}
	}

	// Load logging switch
	db.Where("name = \"app.torrent.log\"").First(&common_config)

	// Prepare TrackerResource
	tr := &TrackerResource{setting: s, user_agents: user_agents, credits: credits, log: common_config.Value == "1"}

	// Initialize IRIS
	iris.OnError(iris.StatusNotFound, func(c *iris.Context) {
		berror(c, "错误：Passkey 不能为空")
	})

	iris.Get("/:passkey", tr.Announcement)

	iris.Listen(s.Listen)
}

func (tr *TrackerResource) Announcement(c *iris.Context) {
	// Get User-Agent
	user_agent := string(c.UserAgent())

	// Get Passkey from url
	passkey := c.Param("passkey")

	// Check parameters
	m := c.URLParams()
	required_params := []string{"info_hash", "peer_id", "port", "uploaded", "downloaded", "left"}
	for _, param_key := range required_params {
		if _, ok := m[param_key]; !ok {
			berror(c, fmt.Sprintf("错误：缺少参数 %s", param_key))
			return
		}
	}

	// Get URL parameters
	event := c.URLParam("event")
	info_hash := c.URLParam("info_hash")
	peer_id := c.URLParam("peer_id")
	port, _ := c.URLParamInt("port")
	uploaded, _ := c.URLParamInt("uploaded")
	downloaded, _ := c.URLParamInt("downloaded")
	left, _ := c.URLParamInt("left")

	// Get client IP
	ips := strings.Split(c.RequestHeader("X-FORWARDED-FOR"), ", ")
	ip := ips[0]
	if ip == "" {
		ip = c.RequestIP()
	}

	if ip == "" {
		berror(c, "错误：无法获取客户端IP")
		return
	}

	// Check if User-Agent allowed
	allowed := false
	for _, v := range tr.user_agents {
		if allowed, _ = regexp.MatchString(v.AgentPattern, user_agent); allowed {
			if len(v.PeerIdPattern) > 0 {
				if allowed, _ = regexp.MatchString(v.PeerIdPattern, peer_id); allowed {
					break
				}
			} else {
				break
			}
		}
	}

	if !allowed {
		berror(c, "错误：客户端不被支持")
		return
	}

	// Start Database connection
	db, err := gorm.Open("mysql", tr.setting.DSN)
	db.LogMode(tr.setting.Debug)

	if err != nil {
		berror(c, "错误：数据库连接失败")
		return
	}

	defer db.Close()

	// Get user info by passkey
	var user AppTorrentUser
	db.Where("passkey = ?", passkey).First(&user)

	if (AppTorrentUser{}) == user {
		berror(c, "错误：无效的 passkey，请尝试重新下载种子")
		return
	}

	// Check if BBS user existed
	var pwuser User
	db.Where("uid = ?", user.Uid).First(&pwuser)

	if (User{}) == pwuser {
		berror(c, "错误：用户不存在，请尝试重新下载种子")
		return
	}

	// Check if BBS user is banned
	var user_ban UserBan
	db.Where("uid = ?", user.Uid).First(&user_ban)

	if (UserBan{}) != user_ban {
		berror(c, fmt.Sprintf("错误：用户已被封禁 %s", user_ban.Reason))
		return
	}

	// Get torrent info by info_hash
	var torrent AppTorrent
	db.Where("info_hash = ?", info_hash).First(&torrent)

	if (AppTorrent{}) == torrent {
		berror(c, "错误：种子信息未注册，可能是已被删除")
		return
	}

	var bbs_thread BbsThread
	db.Where("tid = ?", torrent.Tid).First(&bbs_thread)

	if (BbsThread{}) == bbs_thread {
		berror(c, "错误：种子不存在")
		return
	}

	if bbs_thread.Disabled > 0 && bbs_thread.CreatedUserid != user.Uid {
		if pwuser.Groupid < 3 || pwuser.Groupid > 5 {
			berror(c, "错误：种子已删除或待审核")
			return
		}
	}

	// Log announcement
	if tr.log {
		db.Create(&AppTorrentLog{
			Uid:         user.Uid,
			TorrentId:   torrent.Id,
			Agent:       user_agent,
			Passkey:     passkey,
			InfoHash:    info_hash,
			PeerId:      peer_id,
			Ip:          ip,
			Port:        port,
			Uploaded:    uploaded,
			Downloaded:  downloaded,
			Left:        left,
			AnnouncedAt: time.Now(),
		})
	}

	// Get peers list by torrent
	torrent.Seeders = 0
	torrent.Leechers = 0
	var self AppTorrentPeer
	var peers []AppTorrentPeer
	db.Where("torrent_id = ?", torrent.Id).Find(&peers)

	i := 0
	for _, peer := range peers {
		if peer.Uid == user.Uid {
			// Get self from peers list by Uid
			self = peer
			peers = append(peers[:i], peers[i+1:]...)
		} else {
			// Count seeders and leechers
			if peer.Seeder {
				torrent.Seeders++
			} else {
				torrent.Leechers++
			}
		}

		i++
	}

	if (AppTorrentPeer{}) == self {
		// Create peer if not exist
		self.Uid = user.Uid
		self.TorrentId = torrent.Id
		self.Username = pwuser.Username
		self.Ip = ip
		self.PeerId = peer_id
		self.Port = port
		self.Uploaded = uploaded
		self.Downloaded = downloaded
		self.Left = left
		self.Agent = user_agent
		self.StartedAt = time.Now()
		self.LastAction = time.Now()
	}

	// Check if self is seeder
	self.Seeder = left <= 0

	if self.Seeder {
		torrent.Seeders++
	} else {
		torrent.Leechers++
	}

	// Check if peer is connectable
	conn, err := net.Dial("tcp", fmt.Sprintf("%s:%d", ip, port))
	if err != nil {
		self.Connectable = false
	} else {
		self.Connectable = true
		defer conn.Close()
	}

	// Check if already started
	if self.PeerId != peer_id || self.Ip != ip {
		berror(c, "错误：同一种子禁止多处下载")
		return
	}

	// Get history by torrent ID and Uid
	var history AppTorrentHistory
	db.Where("torrent_id = ? AND uid = ?", torrent.Id, user.Uid).First(&history)

	var rotio float64
	var uploaded_add, downloaded_add int

	if (AppTorrentHistory{}) == history {
		// Create history if not exist
		history = AppTorrentHistory{
			Uid:        user.Uid,
			TorrentId:  torrent.Id,
			Uploaded:   uploaded,
			Downloaded: downloaded,
			Left:       left,
			Leeched:    0,
			Seeded:     0,
		}

		db.Create(&history)
	} else {
		// Calculate increment
		uploaded_add = int(math.Max(0, float64(uploaded-self.Uploaded)))
		downloaded_add = int(math.Max(0, float64(downloaded-self.Downloaded)))

		history.Uploaded = history.Uploaded + uploaded_add
		history.Downloaded = history.Downloaded + downloaded_add

		history.Left = left

		if self.Seeder {
			history.Seeded += int(time.Since(self.LastAction).Seconds())
		} else {
			history.Leeched += int(time.Since(self.LastAction).Seconds())
		}

		db.Save(&history)
	}

	if len(tr.credits) > 0 {
		// Calculate rotio
		if history.Downloaded != 0 {
			rotio = math.Floor(float64(history.Uploaded/history.Downloaded*100)+0.5) / 100
		} else {
			rotio = 1
		}

		// Prepare parameters for credits calculator
		parameters := make(map[string]interface{}, 19)

		parameters["e"] = math.E
		parameters["pi"] = math.Pi
		parameters["phi"] = math.Phi

		var seeding []AppTorrentPeer
		db.Where("uid = ? AND seeder = 1", user.Uid).Find(&seeding)

		var leeching []AppTorrentPeer
		db.Where("uid = ? AND seeder = 0", user.Uid).Find(&leeching)

		var published_torrents []AppTorrent
		db.Where("Owner = ?", user.Uid).Find(&published_torrents)

		var user_data UserData
		db.Where("uid = ?", user.Uid).Find(&user_data)

		var windid_user_data WindidUserData
		db.Where("uid = ?", user.Uid).Find(&windid_user_data)

		parameters["alive"] = int(time.Since(torrent.CreatedAt).Hours() / 24)
		parameters["seeders"] = torrent.Seeders
		parameters["leechers"] = torrent.Leechers
		parameters["size"] = torrent.Size
		parameters["seeding"] = len(seeding)
		parameters["leeching"] = len(leeching)
		parameters["downloaded"] = history.Uploaded
		parameters["downloaded_add"] = downloaded_add
		parameters["uploaded"] = history.Uploaded
		parameters["uploaded_add"] = uploaded_add
		parameters["rotio"] = rotio
		parameters["time"] = int(time.Since(self.StartedAt).Seconds())
		parameters["time_la"] = int(time.Since(self.LastAction).Seconds())
		parameters["time_leeched"] = history.Leeched
		parameters["time_seeded"] = history.Seeded
		parameters["torrents"] = len(published_torrents)

		// Calculate increment of credits
		for k, v := range tr.credits {
			if !v.enabled {
				continue
			}

			credit_key := fmt.Sprintf("Credit%d", k)
			parameters["credit"], _ = reflections.GetField(user_data, credit_key)

			expression, _ := govaluate.NewEvaluableExpressionWithFunctions(v.exp, functions)

			delta, _ := expression.Evaluate(parameters)
			result := parameters["credit"].(float64) + delta.(float64)

			reflections.SetField(&user_data, credit_key, result)
			reflections.SetField(&windid_user_data, credit_key, result)
		}

		// Update credits
		db.Save(&user_data)
		db.Save(&windid_user_data)
	}

	// Update peer
	switch event {
	case "", "started":
		{
			self.Port = port
			self.Uploaded = uploaded
			self.Downloaded = downloaded
			self.Left = left
			self.Agent = user_agent
			self.LastAction = time.Now()

			db.Save(&self)
		}
	case "stopped":
		{
			if self.Id != 0 {
				db.Delete(&self)
			}
		}
	case "completed":
		{
			self.Port = port
			self.Uploaded = uploaded
			self.Downloaded = downloaded
			self.Left = left
			self.Agent = user_agent
			self.FinishedAt = time.Now()
			self.LastAction = time.Now()

			db.Save(&self)
		}
	default:
		{
			berror(c, "错误：客户端发送未知状态")
			return
		}
	}

	// Update torrent
	torrent.UpdatedAt = time.Now()

	db.Save(&torrent)

	// Output peers list to client
	peer_list := PeerList{
		Interval:    840,
		MinInterval: 30,
		Complete:    torrent.Seeders,
		Incomplete:  torrent.Leechers,
		Peers:       peers,
	}

	buf := new(bytes.Buffer)

	bencode.Marshal(buf, peer_list)

	c.Text(200, buf.String())
}

func berror(c *iris.Context, msg string) {
	err := Error{msg}

	buf := new(bytes.Buffer)

	bencode.Marshal(buf, err)

	c.Text(200, buf.String())
}
