package main

import (
	"fmt"
	"log"
	"math/rand"
	"os"
	"regexp"
	"time"

	"github.com/faiface/beep"
	"github.com/faiface/beep/speaker"
	"github.com/faiface/beep/wav"
	"github.com/gorilla/websocket"
	"github.com/mitchellh/copystructure"
)

type Config struct {
	ConditionInterval []int  `yaml:"conditionInterval,omitempty"`
	RandomizeInterval bool   `yaml:"randomizeInterval,omitempty"`
	PauseInterval     int    `yaml:"pauseInterval,omitempty"`
	PlaySound         bool   `yaml:"playSound,omitempty"`
	SoundFile         string `yaml:"soundFile,omitempty"`
	PauseInputMatcher string `yaml:"pauseInputMatcher,omitempty"`
}

type PsychTimer struct {
	config      Config
	soundStream beep.StreamSeekCloser
	soundFormat beep.Format
	conn        *websocket.Conn
	regex       *regexp.Regexp
	matches     []string
	matchBytes  []byte
	ch          chan ServerMessage
}

func NewPsychTimer(c Config, conn *websocket.Conn, ch chan ServerMessage) *PsychTimer {
	t, _ := copystructure.Copy(c)
	n := t.(Config)

	if n.RandomizeInterval {
		rand.Seed(time.Now().UnixNano())

		rand.Shuffle(len(n.ConditionInterval), func(i, j int) {
			n.ConditionInterval[i], n.ConditionInterval[j] = n.ConditionInterval[j], n.ConditionInterval[i]
		})
	}
	f, err := os.Open(n.SoundFile)
	if err != nil {
		log.Fatal(err)
	}

	streamer, format, err := wav.Decode(f)
	if err != nil {
		log.Fatal(err)
	}

	var matcher *regexp.Regexp
	if n.PauseInputMatcher != "" {
		matcher = regexp.MustCompile(n.PauseInputMatcher)
	}

	return &PsychTimer{
		config:      n,
		soundStream: streamer,
		soundFormat: format,
		conn:        conn,
		regex:       matcher,
		ch:          ch}
}

func (p *PsychTimer) playBeep() {
	fmt.Printf("Playing sound %s\n", p.config.SoundFile)

	p.soundStream.Seek(0)
	speaker.Init(p.soundFormat.SampleRate, p.soundFormat.SampleRate.N(time.Second/10))
	speaker.Play(p.soundStream)
}

func (p *PsychTimer) RunOne(ID string) {
	p.ch <- ServerMessage{
		Kind:    "BEGIN",
		Message: ID,
	}
	for _, v := range p.config.ConditionInterval {
		p.ch <- ServerMessage{
			Kind:    "INFO",
			Message: fmt.Sprintf("Starting new interval for %s", ID),
		}
		if p.config.PlaySound {
			p.playBeep()
		}
		p.ch <- ServerMessage{
			Kind:    "INFO",
			Message: fmt.Sprintf("Waiting %d seconds for math\n", v),
		}
		time.Sleep(time.Duration(v) * time.Second)
		if p.config.PlaySound {
			p.playBeep()
		}
		p.ch <- ServerMessage{
			Kind:    "INFO",
			Message: fmt.Sprintf("Waiting %d seconds for input\n", p.config.PauseInterval),
		}
		time.Sleep(time.Duration(p.config.PauseInterval) * time.Second)
		p.ch <- ServerMessage{
			Kind:    "INFO",
			Message: fmt.Sprintf("Done with interval for %s", ID),
		}
	}
	p.ch <- ServerMessage{
		Kind:    "END",
		Message: ID,
	}
}

func (p *PsychTimer) AddKey(k string, b byte) {
	p.matchBytes = append(p.matchBytes, b)
	fmt.Println(p.matchBytes)

	if p.regex != nil {
		m := p.regex.Find(p.matchBytes)
		if m != nil {
			p.matches = append(p.matches, string(m))
			p.matchBytes = nil
			p.ch <- ServerMessage{
				Kind:    "INFO",
				Message: fmt.Sprintf("Matched number: %s", p.matches[len(p.matches)-1]),
			}
		}
	}
}
