package main

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	term "github.com/nsf/termbox-go"
)

// plug is the representation of the keybinding and plug pairing
type plug struct {
	IPAddress  string
	TriggerKey int
	Model      string
	Name       string
	mtx        *sync.Mutex
	On         bool
	lastCmd    time.Time
}

// all of the structs below are just to conform to the sysinfo json result
type system struct {
	command `json:"system"`
}

type command struct {
	info `json:"get_sysinfo"`
}

type info struct {
	Alias           string  `json:"alias,omitempty"`
	SoftwareVersion string  `json:"sw_veri,omitempty"`
	HardwareVersion string  `json:"hw_ver,omitempty"`
	Model           string  `json:"model,omitempty"`
	DeviceID        string  `json:"deviceId,omitempty"`
	OemID           string  `json:"oemId,omitempty"`
	HardwareID      string  `json:"hwId,omitempty"`
	Rssi            float64 `json:"rssi,omitempty"`
	Longitude       float64 `json:"longitude,omitempty"`
	Latitude        float64 `json:"latitude,omitempty"`
	Updating        int     `json:"updating,omitempty"`
	LEDOff          int     `json:"led_off,omitempty"`
	RelayState      int     `json:"relay_state,omitempty"`
	OnTime          int     `json:"on_time,omitempty"`
	ActiveMode      string  `json:"active_mode,omitempty"`
	IconHash        string  `json:"icon_hash,omitempty"`
	ErrorCode       int     `json:"err_code,omitempty"`
}

func main() {
	if len(os.Args) != 2 {
		fmt.Println("Usage: kasa-internal <ip>:<key>,<ip>:<key>")
		os.Exit(1)
	}

	err := term.Init()
	if err != nil {
		panic(err)
	}
	defer term.Close()

	// mapping should be in the form: <ip addr>:<key>,<ip addr>:<key>
	mapping := os.Args[1]
	plugs := processMapping(mapping)
	getSystemInfo(plugs...)

	for {
		fmt.Println("Listening for input")
		event := term.PollEvent()
		eventType := event.Type

		if eventType != term.EventKey {
			continue
		}

		if event.Key == term.KeyCtrlC {
			return
		}

		for _, plug := range plugs {
			if term.Key(plug.TriggerKey) == event.Key {
				_ = term.Sync()
				err := plug.toggle()
				if err != nil {
					fmt.Printf("could not toggle switch %s; %v", plug.Name, err)
					continue
				}

			}
		}
	}
}

// This takes a long time.
func getSystemInfo(plugs ...*plug) {
	for _, plug := range plugs {
		info, err := plug.systemInfo()
		if err != nil {
			fmt.Println(err)
			return
		}

		plug.Name = info.Alias
		plug.Model = info.Model
		plug.On = int2bool(info.RelayState)
		fmt.Printf("Found plug: %s\n", plug.Name)
	}
}

func int2bool(r int) bool {
	return r == 1
}

func processMapping(m string) []*plug {
	mappingSlice := strings.Split(m, ",")

	plugs := []*plug{}

	for _, mapping := range mappingSlice {
		IPKeyPair := strings.Split(mapping, ":")
		triggerKey, err := strconv.Atoi(IPKeyPair[1])
		if err != nil {
			panic(err)
		}
		plugs = append(plugs, &plug{
			IPAddress:  IPKeyPair[0],
			TriggerKey: triggerKey,
			mtx:        &sync.Mutex{},
		})
	}

	return plugs
}

func (p *plug) systemInfo() (system, error) {
	payload := `{"system":{"get_sysinfo":{}}}`
	results, err := p.sendCmd(payload)
	if err != nil {
		return system{}, err
	}

	var info system
	err = json.Unmarshal(results, &info)
	if err != nil {
		return system{}, err
	}

	return info, nil
}

func (p *plug) turnOn() (err error) {
	payload := `{"system":{"set_relay_state":{"state":1}}}`
	_, err = p.sendCmd(payload)
	return
}

func (p *plug) turnOff() (err error) {
	payload := `{"system":{"set_relay_state":{"state":0}}}`
	_, err = p.sendCmd(payload)
	return
}

func (p *plug) toggle() (err error) {
	if p.On {
		err = p.turnOff()
		p.On = false
		fmt.Printf("Toggled: %s %s\n", p.Name, time.Now().Format("01-02 15:04:05"))
		return
	}

	err = p.turnOn()
	p.On = true
	fmt.Printf("Toggled: %s %s\n", p.Name, time.Now().Format("01-02 15:04:05"))
	return
}

// sendCmd handles the communication with the plug.
func (p *plug) sendCmd(data string) ([]byte, error) {
	// protect against sending too many commands at once
	p.mtx.Lock()
	defer func() {
		p.lastCmd = time.Now()
		p.mtx.Unlock()
	}()
	if time.Since(p.lastCmd) < time.Millisecond*500 {
		time.Sleep(time.Millisecond * 500)
	}

	res := make([]byte, 2048)

	// connect to plug
	conn, err := net.Dial("tcp", p.IPAddress+":9999")
	if err != nil {
		return res, fmt.Errorf("connecting to plug: %w", err)
	}
	defer conn.Close()

	// set timeout
	if err := conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		return res, fmt.Errorf("setting timeout: %w", err)
	}

	payload := encrypt([]byte(data))

	if _, err := conn.Write(payload); err != nil {
		return res, fmt.Errorf("writing payload: %w", err)
	}

	// receive, decrypt response
	i, err := conn.Read(res)
	if err != nil {
		return res, err
	}
	decrypted := decrypt(res[:i]) // only include the bytes that were read
	return decrypted, nil
}

// encrypt follows the autokey cipher used by the HS1xx to encrypt commands.
func encrypt(bx []byte) []byte {
	key := 171
	res := make([]byte, 4)
	binary.BigEndian.PutUint32(res, uint32(len(bx))) // equivalent in python: struct.pack('>I', len(cmd))

	for i := range bx {
		b := key ^ int(bx[i])
		key = b
		res = append(res, byte(b))
	}
	return res
}

// decrypt follows the autokey cipher used by the HS1xx to decrypt commands.
func decrypt(bx []byte) []byte {
	key := 171
	var res []byte

	for i := 4; i < len(bx); i++ { // first 4 bytes are padding
		b := key ^ int(bx[i])
		key = int(bx[i])
		res = append(res, byte(b))
	}
	return res
}
