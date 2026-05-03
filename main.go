package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/csv"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf16"
)

const (
	defaultMasterServer = "http://master.kamremake.com/"
	protocolRevision    = "r16000"
	defaultGameRevision = "r16020"

	maxQueries    = 16
	maxLobbySlots = 14

	mkIndexOnServer      = 4
	mkNetProtocolVersion = 7
	mkGetServerInfo      = 20
	mkServerInfo         = 21
	mkPing               = 23
	mkPong               = 24

	netAddressServer = -4
)

var gameStateTextIDs = []string{"mgsNone", "mgsLobby", "mgsLoading", "mgsGame"}
var playerTypeTextIDs = []string{"nptHuman", "nptClosed", "nptComputerClassic", "nptComputerAdvanced"}
var serverTypeTextIDs = []string{"mstClient", "mstDedicated", "mstLocal"}
var missionDifficultyTextIDs = []string{"mdNone", "mdEasy3", "mdEasy2", "mdEasy1", "mdNormal", "mdHard1", "mdHard2", "mdHard3"}
var wonOrLostTextIDs = []string{"wolNone", "wolWon", "wolLost"}

type serverInfo struct {
	Name       string
	IP         string
	Port       int
	ServerType byte
	OS         string
	Ping       uint16
}

type playerInfo struct {
	Name        string
	Color       uint32
	Connected   bool
	LangCode    string
	Team        int32
	IsSpectator bool
	IsHost      bool
	PlayerType  byte
	WonOrLost   byte
}

type gameOptions struct {
	Peacetime         uint16
	SpeedPT           float32
	SpeedAfterPT      float32
	RandomSeed        int32
	MissionDifficulty byte
}

type gameInfo struct {
	GameState      byte
	PasswordLocked bool
	PlayerCount    byte
	GameOptions    gameOptions
	Players        []playerInfo
	Description    string
	Map            string
	GameTime       float64
}

type roomInfo struct {
	Server       serverInfo
	GameRevision uint16
	RoomID       int
	OnlyRoom     bool
	GameInfo     gameInfo
}

type serverCache struct {
	UpdatedAt time.Time    `json:"updatedAt"`
	Servers   []serverInfo `json:"servers"`
}

type pollResult struct {
	server serverInfo
	rooms  []roomInfo
}

type outputPlayer struct {
	Name        string `json:"Name"`
	Color       string `json:"Color"`
	Connected   bool   `json:"Connected"`
	LangCode    string `json:"LangCode"`
	Team        int32  `json:"Team"`
	IsSpectator bool   `json:"IsSpectator"`
	IsHost      bool   `json:"IsHost"`
	PlayerType  string `json:"PlayerType"`
	WonOrLost   string `json:"WonOrLost"`
}

type outputGameOptions struct {
	Peacetime         uint16  `json:"Peacetime"`
	SpeedPT           float32 `json:"SpeedPT"`
	SpeedAfterPT      float32 `json:"SpeedAfterPT"`
	RandomSeed        int32   `json:"RandomSeed"`
	MissionDifficulty string  `json:"MissionDifficulty"`
}

type outputGameInfo struct {
	GameState      string            `json:"GameState"`
	PasswordLocked bool              `json:"PasswordLocked"`
	PlayerCount    byte              `json:"PlayerCount"`
	GameOptions    outputGameOptions `json:"GameOptions"`
	Players        []outputPlayer    `json:"Players"`
	Description    string            `json:"Description"`
	Map            string            `json:"Map"`
	GameTime       string            `json:"GameTime"`
}

type outputServer struct {
	Name       string `json:"Name"`
	IP         string `json:"IP"`
	Port       int    `json:"Port"`
	ServerType string `json:"ServerType"`
	OS         string `json:"OS"`
	Ping       uint16 `json:"Ping"`
}

type outputRoom struct {
	Server       outputServer   `json:"Server"`
	GameRevision uint16         `json:"GameRevision"`
	RoomID       int            `json:"RoomID"`
	OnlyRoom     bool           `json:"OnlyRoom"`
	GameInfo     outputGameInfo `json:"GameInfo"`
}

type outputJSON struct {
	RoomCount int          `json:"RoomCount"`
	Rooms     []outputRoom `json:"Rooms"`
}

func main() {
	master := flag.String("master", defaultMasterServer, "KaM Remake master server URL")
	timeout := flag.Duration("timeout", 6*time.Second, "total polling timeout")
	masterTimeout := flag.Duration("masterTimeout", 2*time.Second, "master server request timeout")
	serverCachePath := flag.String("serverCache", "servers-cache.json", "server list cache file")
	gameRevision := flag.String("gameRevision", defaultGameRevision, "KaM Remake game revision")
	includeEmptyRooms := flag.Bool("includeEmptyRooms", false, "include rooms without players")
	flag.Parse()

	masterCtx, cancelMaster := context.WithTimeout(context.Background(), minDuration(*masterTimeout, *timeout))
	servers, err := fetchServerList(masterCtx, *master, *gameRevision)
	cancelMaster()
	if err == nil && len(servers) == 0 {
		err = errors.New("master server returned empty server list")
	}
	if err == nil {
		if err := saveServerCache(*serverCachePath, servers); err != nil {
			fmt.Fprintf(os.Stderr, "failed to save server cache: %v\n", err)
		}
	} else {
		fmt.Fprintf(os.Stderr, "failed to fetch master server list: %v\n", err)
		servers, err = loadServerCache(*serverCachePath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to load server cache: %v\n", err)
			os.Exit(1)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	rooms, aliveServers := pollServers(ctx, servers, *timeout)
	if err := saveServerCache(*serverCachePath, aliveServers); err != nil {
		fmt.Fprintf(os.Stderr, "failed to save live server cache: %v\n", err)
	}
	out := buildOutput(rooms, *includeEmptyRooms)

	enc := json.NewEncoder(os.Stdout)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(out); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func fetchServerList(ctx context.Context, master string, gameRevision string) ([]serverInfo, error) {
	base, err := url.Parse(master)
	if err != nil {
		return nil, err
	}
	queryURL, err := base.Parse("serverquery.php")
	if err != nil {
		return nil, err
	}
	q := queryURL.Query()
	q.Set("rev", protocolRevision)
	q.Set("coderev", gameRevision)
	queryURL.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, queryURL.String(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("master server returned %s", resp.Status)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	reader := csv.NewReader(strings.NewReader(string(body)))
	reader.FieldsPerRecord = -1
	records, err := reader.ReadAll()
	if err != nil {
		return nil, err
	}

	servers := make([]serverInfo, 0, len(records))
	for _, rec := range records {
		if len(rec) != 5 {
			continue
		}
		port, err := strconv.Atoi(strings.TrimSpace(rec[2]))
		if err != nil {
			continue
		}
		servers = append(servers, serverInfo{
			Name:       strings.TrimSpace(rec[0]),
			IP:         strings.TrimSpace(rec[1]),
			Port:       port,
			ServerType: getServerType(strings.TrimSpace(rec[3])),
			OS:         strings.TrimSpace(rec[4]),
		})
	}
	return servers, nil
}

func loadServerCache(path string) ([]serverInfo, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var cache serverCache
	if err := json.NewDecoder(f).Decode(&cache); err != nil {
		return nil, err
	}
	if len(cache.Servers) == 0 {
		return nil, errors.New("server cache is empty")
	}
	return cache.Servers, nil
}

func saveServerCache(path string, servers []serverInfo) error {
	cache := serverCache{
		UpdatedAt: time.Now().UTC(),
		Servers:   dedupeServers(servers),
	}
	data, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func dedupeServers(servers []serverInfo) []serverInfo {
	seen := make(map[string]struct{}, len(servers))
	out := make([]serverInfo, 0, len(servers))
	for _, srv := range servers {
		key := net.JoinHostPort(srv.IP, strconv.Itoa(srv.Port))
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		srv.Ping = 0
		out = append(out, srv)
	}
	return out
}

func pollServers(ctx context.Context, servers []serverInfo, timeout time.Duration) ([]roomInfo, []serverInfo) {
	sem := make(chan struct{}, maxQueries)
	results := make(chan pollResult, len(servers))
	var wg sync.WaitGroup

pollLoop:
	for _, srv := range servers {
		select {
		case <-ctx.Done():
			break pollLoop
		case sem <- struct{}{}:
		}

		wg.Add(1)
		go func(s serverInfo) {
			defer wg.Done()
			defer func() { <-sem }()
			rooms, err := queryServer(ctx, s, minDuration(5*time.Second, timeout))
			if err == nil {
				results <- pollResult{server: s, rooms: rooms}
			}
		}(srv)
	}

	wg.Wait()
	close(results)

	var rooms []roomInfo
	aliveServers := make([]serverInfo, 0, len(servers))
	for result := range results {
		rooms = append(rooms, result.rooms...)
		aliveServers = append(aliveServers, result.server)
	}
	return rooms, aliveServers
}

func queryServer(ctx context.Context, srv serverInfo, timeout time.Duration) ([]roomInfo, error) {
	dialer := net.Dialer{Timeout: timeout}
	conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(srv.IP, strconv.Itoa(srv.Port)))
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	deadline := time.Now().Add(timeout)
	_ = conn.SetDeadline(deadline)

	var indexOnServer int16
	var gotIndex bool
	var pingStarted time.Time
	var buffer []byte
	tmp := make([]byte, 4096)

	for {
		n, err := conn.Read(tmp)
		if err != nil {
			return nil, err
		}
		buffer = append(buffer, tmp[:n]...)

		packets, rest, err := parseCumulativePackets(buffer)
		if err != nil {
			return nil, err
		}
		buffer = rest

		for _, packet := range packets {
			if len(packet.data) == 0 {
				continue
			}
			kind := packet.data[0]
			payload := packet.data[1:]

			switch kind {
			case mkNetProtocolVersion:
				rev, err := newStreamReader(payload).readAnsiString()
				if err != nil || rev != protocolRevision {
					return nil, fmt.Errorf("server %s:%d uses protocol %q", srv.IP, srv.Port, rev)
				}
			case mkIndexOnServer:
				r := newStreamReader(payload)
				idx, err := r.readInt16()
				if err != nil {
					return nil, err
				}
				indexOnServer = idx
				gotIndex = true
				pingStarted = time.Now()
				if err := sendPacket(conn, indexOnServer, netAddressServer, mkGetServerInfo, nil); err != nil {
					return nil, err
				}
			case mkServerInfo:
				if !pingStarted.IsZero() {
					srv.Ping = clampWord(time.Since(pingStarted).Milliseconds())
				}
				return parseServerInfo(srv, payload)
			case mkPing:
				if gotIndex {
					_ = sendPacket(conn, indexOnServer, netAddressServer, mkPong, nil)
				}
			}
		}
	}
}

type packet struct {
	sender int16
	data   []byte
}

func parseCumulativePackets(buf []byte) ([]packet, []byte, error) {
	var packets []packet
	pos := 0
	for pos < len(buf) {
		count := int(buf[pos])
		if count == 0 {
			pos++
			continue
		}

		packetStart := pos + 1
		readPos := packetStart
		localPackets := make([]packet, 0, count)
		for i := 0; i < count; i++ {
			if len(buf)-readPos < 6 {
				return packets, buf[pos:], nil
			}
			sender := int16(binary.LittleEndian.Uint16(buf[readPos:]))
			length := int(binary.LittleEndian.Uint16(buf[readPos+4:]))
			if length < 0 || length > 20480 {
				return nil, nil, errors.New("corrupt packet length")
			}
			if len(buf)-readPos-6 < length {
				return packets, buf[pos:], nil
			}
			data := append([]byte(nil), buf[readPos+6:readPos+6+length]...)
			localPackets = append(localPackets, packet{sender: sender, data: data})
			readPos += 6 + length
		}
		packets = append(packets, localPackets...)
		pos = readPos
	}
	return packets, nil, nil
}

func sendPacket(w io.Writer, sender int16, recipient int16, kind byte, payload []byte) error {
	msgLen := 1 + len(payload)
	buf := bytes.NewBuffer(make([]byte, 0, 6+msgLen))
	_ = binary.Write(buf, binary.LittleEndian, sender)
	_ = binary.Write(buf, binary.LittleEndian, recipient)
	_ = binary.Write(buf, binary.LittleEndian, uint16(msgLen))
	buf.WriteByte(kind)
	buf.Write(payload)
	_, err := w.Write(buf.Bytes())
	return err
}

func parseServerInfo(srv serverInfo, data []byte) ([]roomInfo, error) {
	r := newStreamReader(data)
	roomCount, err := r.readInt32()
	if err != nil {
		return nil, err
	}

	rooms := make([]roomInfo, 0, roomCount)
	for i := int32(0); i < roomCount; i++ {
		roomID, err := r.readInt32()
		if err != nil {
			return nil, err
		}
		gameRevision, err := r.readUint16()
		if err != nil {
			return nil, err
		}
		game, err := parseGameInfo(r)
		if err != nil {
			return nil, err
		}
		rooms = append(rooms, roomInfo{
			Server:       srv,
			GameRevision: gameRevision,
			RoomID:       int(roomID),
			OnlyRoom:     roomCount == 1,
			GameInfo:     game,
		})
	}
	return rooms, nil
}

func parseGameInfo(r *streamReader) (gameInfo, error) {
	var g gameInfo
	var err error

	if g.GameState, err = r.readByte(); err != nil {
		return g, err
	}
	if g.PasswordLocked, err = r.readBool(); err != nil {
		return g, err
	}
	if g.PlayerCount, err = r.readByte(); err != nil {
		return g, err
	}
	if g.GameOptions, err = readGameOptions(r); err != nil {
		return g, err
	}

	count := int(g.PlayerCount)
	if count > maxLobbySlots {
		count = maxLobbySlots
	}
	g.Players = make([]playerInfo, 0, count)
	for i := 0; i < int(g.PlayerCount); i++ {
		p := playerInfo{}
		if p.Name, err = r.readAnsiString(); err != nil {
			return g, err
		}
		if p.Color, err = r.readUint32(); err != nil {
			return g, err
		}
		if p.Connected, err = r.readBool(); err != nil {
			return g, err
		}
		if p.LangCode, err = r.readAnsiString(); err != nil {
			return g, err
		}
		if p.Team, err = r.readInt32(); err != nil {
			return g, err
		}
		if p.IsSpectator, err = r.readBool(); err != nil {
			return g, err
		}
		if p.IsHost, err = r.readBool(); err != nil {
			return g, err
		}
		if p.PlayerType, err = r.readByte(); err != nil {
			return g, err
		}
		if p.WonOrLost, err = r.readByte(); err != nil {
			return g, err
		}
		if i < maxLobbySlots {
			g.Players = append(g.Players, p)
		}
	}
	if g.Description, err = r.readUnicodeString(); err != nil {
		return g, err
	}
	if g.Map, err = r.readUnicodeString(); err != nil {
		return g, err
	}
	if g.GameTime, err = r.readFloat64(); err != nil {
		return g, err
	}
	return g, nil
}

func readGameOptions(r *streamReader) (gameOptions, error) {
	var options gameOptions
	var err error

	if options.Peacetime, err = r.readUint16(); err != nil {
		return options, err
	}
	if options.SpeedPT, err = r.readFloat32(); err != nil {
		return options, err
	}
	if options.SpeedAfterPT, err = r.readFloat32(); err != nil {
		return options, err
	}
	if options.RandomSeed, err = r.readInt32(); err != nil {
		return options, err
	}
	options.MissionDifficulty, err = r.readByte()
	return options, err
}

func buildOutput(rooms []roomInfo, includeEmptyRooms bool) outputJSON {
	out := outputJSON{Rooms: make([]outputRoom, 0, len(rooms))}
	for _, room := range rooms {
		if !includeEmptyRooms && room.GameInfo.PlayerCount == 0 {
			continue
		}

		players := make([]outputPlayer, 0, len(room.GameInfo.Players))
		for _, player := range room.GameInfo.Players {
			players = append(players, outputPlayer{
				Name:        player.Name,
				Color:       formatPlayerColor(player.Color),
				Connected:   player.Connected,
				LangCode:    player.LangCode,
				Team:        player.Team,
				IsSpectator: player.IsSpectator,
				IsHost:      player.IsHost,
				PlayerType:  playerTypeName(player.PlayerType),
				WonOrLost:   wonOrLostName(player.WonOrLost),
			})
		}

		out.Rooms = append(out.Rooms, outputRoom{
			Server: outputServer{
				Name:       stripColor(room.Server.Name),
				IP:         room.Server.IP,
				Port:       room.Server.Port,
				ServerType: serverTypeName(room.Server.ServerType),
				OS:         room.Server.OS,
				Ping:       room.Server.Ping,
			},
			GameRevision: room.GameRevision,
			RoomID:       room.RoomID,
			OnlyRoom:     room.OnlyRoom,
			GameInfo: outputGameInfo{
				GameState:      gameStateName(room.GameInfo.GameState),
				PasswordLocked: room.GameInfo.PasswordLocked,
				PlayerCount:    room.GameInfo.PlayerCount,
				GameOptions: outputGameOptions{
					Peacetime:         room.GameInfo.GameOptions.Peacetime,
					SpeedPT:           room.GameInfo.GameOptions.SpeedPT,
					SpeedAfterPT:      room.GameInfo.GameOptions.SpeedAfterPT,
					RandomSeed:        room.GameInfo.GameOptions.RandomSeed,
					MissionDifficulty: missionDifficultyName(room.GameInfo.GameOptions.MissionDifficulty),
				},
				Players:     players,
				Description: room.GameInfo.Description,
				Map:         room.GameInfo.Map,
				GameTime:    formatGameTime(room.GameInfo.GameTime),
			},
		})
	}
	out.RoomCount = len(out.Rooms)
	return out
}

type streamReader struct {
	data []byte
	pos  int
}

func newStreamReader(data []byte) *streamReader {
	return &streamReader{data: data}
}

func (r *streamReader) read(n int) ([]byte, error) {
	if n < 0 || len(r.data)-r.pos < n {
		return nil, io.ErrUnexpectedEOF
	}
	out := r.data[r.pos : r.pos+n]
	r.pos += n
	return out, nil
}

func (r *streamReader) readByte() (byte, error) {
	b, err := r.read(1)
	if err != nil {
		return 0, err
	}
	return b[0], nil
}

func (r *streamReader) readBool() (bool, error) {
	b, err := r.readByte()
	return b != 0, err
}

func (r *streamReader) readInt16() (int16, error) {
	b, err := r.read(2)
	return int16(binary.LittleEndian.Uint16(b)), err
}

func (r *streamReader) readUint16() (uint16, error) {
	b, err := r.read(2)
	return binary.LittleEndian.Uint16(b), err
}

func (r *streamReader) readInt32() (int32, error) {
	b, err := r.read(4)
	return int32(binary.LittleEndian.Uint32(b)), err
}

func (r *streamReader) readUint32() (uint32, error) {
	b, err := r.read(4)
	return binary.LittleEndian.Uint32(b), err
}

func (r *streamReader) readFloat32() (float32, error) {
	b, err := r.read(4)
	return math.Float32frombits(binary.LittleEndian.Uint32(b)), err
}

func (r *streamReader) readFloat64() (float64, error) {
	b, err := r.read(8)
	return math.Float64frombits(binary.LittleEndian.Uint64(b)), err
}

func (r *streamReader) readAnsiString() (string, error) {
	n, err := r.readUint16()
	if err != nil {
		return "", err
	}
	b, err := r.read(int(n))
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func (r *streamReader) readUnicodeString() (string, error) {
	n, err := r.readUint16()
	if err != nil {
		return "", err
	}
	b, err := r.read(int(n) * 2)
	if err != nil {
		return "", err
	}
	u := make([]uint16, n)
	for i := range u {
		u[i] = binary.LittleEndian.Uint16(b[i*2:])
	}
	return string(utf16.Decode(u)), nil
}

func gameStateName(id byte) string {
	if int(id) < len(gameStateTextIDs) {
		return gameStateTextIDs[id]
	}
	return fmt.Sprintf("mgsUnknown%d", id)
}

func playerTypeName(id byte) string {
	if int(id) < len(playerTypeTextIDs) {
		return playerTypeTextIDs[id]
	}
	return fmt.Sprintf("nptUnknown%d", id)
}

func serverTypeName(id byte) string {
	if int(id) < len(serverTypeTextIDs) {
		return serverTypeTextIDs[id]
	}
	return fmt.Sprintf("mstUnknown%d", id)
}

func missionDifficultyName(id byte) string {
	if int(id) < len(missionDifficultyTextIDs) {
		return missionDifficultyTextIDs[id]
	}
	return fmt.Sprintf("mdUnknown%d", id)
}

func wonOrLostName(id byte) string {
	if int(id) < len(wonOrLostTextIDs) {
		return wonOrLostTextIDs[id]
	}
	return fmt.Sprintf("wolUnknown%d", id)
}

func getServerType(dedicated string) byte {
	if dedicated == "0" {
		return 0
	}
	return 1
}

func clampWord(value int64) uint16 {
	if value < 0 {
		return 0
	}
	if value > math.MaxUint16 {
		return math.MaxUint16
	}
	return uint16(value)
}

func stripColor(s string) string {
	var b strings.Builder
	skipping := false
	for i := 0; i < len(s); i++ {
		if i+1 < len(s) && ((s[i] == '[' && s[i+1] == '$') || (s[i] == '[' && s[i+1] == ']')) {
			skipping = true
		}
		if !skipping {
			b.WriteByte(s[i])
		}
		if skipping && s[i] == ']' {
			skipping = false
		}
	}
	return b.String()
}

func formatGameTime(delphiDateTime float64) string {
	if delphiDateTime < 0 {
		return ""
	}
	totalSeconds := int64(math.Floor(delphiDateTime*24*60*60 + 0.5))
	h := totalSeconds / 3600
	m := (totalSeconds / 60) % 60
	s := totalSeconds % 60
	return fmt.Sprintf("%02d:%02d:%02d", h, m, s)
}

func formatPlayerColor(color uint32) string {
	r := byte(color)
	g := byte(color >> 8)
	b := byte(color >> 16)
	return fmt.Sprintf("#%02X%02X%02X", r, g, b)
}

func minDuration(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}
