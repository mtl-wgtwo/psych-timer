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

// label: Baseline
// time: 300
// playSound: true
// pauseAfter: 30
// inputMatcher: "([0-9]+)[\n\r]"
type Interval struct {
	Label        string         `yaml:"label,omitempty"`
	Time         int            `yaml:"time,omitempty"` //seconds
	PlaySound    bool           `yaml:"playSound,omitempty"`
	PauseAfter   int            `yaml:"pauseAfter,omitempty"` //seconds
	InputMatcher string         `yaml:"inputMatcher,omitempty"`
	regexMatcher *regexp.Regexp `yaml:"regexMatcher,omitempty"`
}

type Config struct {
	Intervals         []Interval `yaml:"intervals,omitempty"`
	RandomizeInterval bool       `yaml:"randomizeInterval,omitempty"`
	SoundFile         string     `yaml:"soundFile,omitempty"`
	StudyLabel        string     `yaml:"studyLabel,omitempty"`
}

type PsychTimer struct {
	config          Config
	soundStream     beep.StreamSeekCloser
	soundFormat     beep.Format
	conn            *websocket.Conn
	matches         []string
	matchBytes      []byte
	ch              chan ServerMessage
	currentInterval *Interval
	currentFile     *MindwareFile
}

func NewPsychTimer(c Config, conn *websocket.Conn, ch chan ServerMessage) *PsychTimer {
	t, _ := copystructure.Copy(c)
	n := t.(Config)

	for i, interval := range n.Intervals {
		if interval.InputMatcher != "" {
			n.Intervals[i].regexMatcher = regexp.MustCompile(interval.InputMatcher)
		}
	}

	if n.RandomizeInterval {
		r := rand.New(rand.NewSource(time.Now().UnixNano()))

		r.Shuffle(len(n.Intervals), func(i, j int) {
			n.Intervals[i], n.Intervals[j] = n.Intervals[j], n.Intervals[i]
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

	fmt.Printf("Final config: %+v\n", n)

	return &PsychTimer{
		config:      n,
		soundStream: streamer,
		soundFormat: format,
		conn:        conn,
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
	p.currentFile = NewMindwareFile(ID + ".tsv")
	defer p.currentFile.Close()

	for i, v := range p.config.Intervals {
		p.currentInterval = &p.config.Intervals[i]
		p.ch <- ServerMessage{
			Kind:    "INFO",
			Message: fmt.Sprintf("Starting new interval for %s: %s", ID, v.Label),
		}
		p.currentFile.WriteEvent("PsychTimer/"+p.config.StudyLabel+" Event", "Start Interval: "+v.Label)

		// Interval starts
		if v.PlaySound {
			p.playBeep()
		}
		p.ch <- ServerMessage{
			Kind:    "INFO",
			Message: fmt.Sprintf("Interval wait period: %d seconds\n", v.Time),
		}
		time.Sleep(time.Duration(v.Time) * time.Second)
		if v.PlaySound {
			p.playBeep()
		}

		// Optional pause interval
		if v.PauseAfter > 0 {
			p.ch <- ServerMessage{
				Kind:    "INFO",
				Message: fmt.Sprintf("Interval post wait period: %d seconds\n", v.PauseAfter),
			}
			p.currentFile.WriteEvent("PsychTimer/"+p.config.StudyLabel+" Event", "Start Pause: "+v.Label)
			time.Sleep(time.Duration(v.PauseAfter) * time.Second)
		}
		p.ch <- ServerMessage{
			Kind:    "INFO",
			Message: fmt.Sprintf("Done with interval for %s: %s", ID, v.Label),
		}
		p.currentFile.WriteEvent("PsychTimer/"+p.config.StudyLabel+" Event", "End Interval: "+v.Label)
	}
	p.ch <- ServerMessage{
		Kind:    "END",
		Message: ID,
	}
}

func (p *PsychTimer) AddKey(k string, b byte) {
	p.matchBytes = append(p.matchBytes, b)
	fmt.Println(p.matchBytes)
	r := p.currentInterval.regexMatcher

	if r != nil {
		m := r.FindSubmatch(p.matchBytes)
		if m != nil && len(m) == 2 {
			p.matches = append(p.matches, string(m[1]))
			p.matchBytes = nil
			val := p.matches[len(p.matches)-1]
			p.ch <- ServerMessage{
				Kind:    "INFO",
				Message: fmt.Sprintf("Matched number: %s", val),
			}
			p.currentFile.WriteEvent("PsychTimer/"+p.config.StudyLabel+" Value", val)
		}
	}
}
