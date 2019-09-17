package main

import (
	"fmt"
	"log"
	"math/rand"
	"os"
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
}

type PsychTimer struct {
	C    Config
	S    beep.StreamSeekCloser
	F    beep.Format
	Conn *websocket.Conn
}

func NewPsychTimer(c Config, conn *websocket.Conn) *PsychTimer {
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

	return &PsychTimer{n, streamer, format, conn}
}

func (p *PsychTimer) playBeep() {
	fmt.Printf("Playing sound %s\n", p.C.SoundFile)

	p.S.Seek(0)
	speaker.Init(p.F.SampleRate, p.F.SampleRate.N(time.Second/10))
	speaker.Play(p.S)
}

func (p *PsychTimer) RunOne(ID string, ch chan ServerMessage) {
	ch <- ServerMessage{
		Kind:    "BEGIN",
		Message: ID,
	}
	for _, v := range p.C.ConditionInterval {
		ch <- ServerMessage{
			Kind:    "INFO",
			Message: fmt.Sprintf("Starting new interval for %s", ID),
		}
		if p.C.PlaySound {
			p.playBeep()
		}
		ch <- ServerMessage{
			Kind:    "INFO",
			Message: fmt.Sprintf("Waiting %d seconds for math\n", v),
		}
		time.Sleep(time.Duration(v) * time.Second)
		if p.C.PlaySound {
			p.playBeep()
		}
		ch <- ServerMessage{
			Kind:    "INFO",
			Message: fmt.Sprintf("Waiting %d seconds for input\n", p.C.PauseInterval),
		}
		time.Sleep(time.Duration(p.C.PauseInterval) * time.Second)
		ch <- ServerMessage{
			Kind:    "INFO",
			Message: fmt.Sprintf("Done with interval for %s", ID),
		}
	}
	ch <- ServerMessage{
		Kind:    "END",
		Message: ID,
	}
}
