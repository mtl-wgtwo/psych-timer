package main

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"

	"github.com/faiface/beep"
	"github.com/faiface/beep/speaker"
	"github.com/faiface/beep/wav"
	"github.com/gorilla/websocket"
	"github.com/mitchellh/copystructure"
	log "github.com/sirupsen/logrus"
)

type Pause struct {
	Type string `yaml:"type,omitempty"`
	Time int    `yaml:"time,omitempty"` //seconds
	wait chan bool
}

type Interval struct {
	Label        string   `yaml:"label,omitempty"`
	Time         int      `yaml:"time,omitempty"` //seconds
	PlaySound    bool     `yaml:"playSound,omitempty"`
	PauseAfter   []*Pause `yaml:"pauseAfter,omitempty"`
	PauseBefore  []*Pause `yaml:"pauseBefore,omitempty"`
	InputMatcher string   `yaml:"inputMatcher,omitempty"`
	Instructions string   `yaml:"instructions,omitempty"`

	regexMatcher *regexp.Regexp `yaml:"regexMatcher,omitempty"`
}

type Config struct {
	Intervals         []Interval `yaml:"intervals,omitempty" json:"intervals,omitempty"`
	RandomizeInterval bool       `yaml:"randomizeInterval,omitempty" json:"randomize_interval,omitempty"`
	PreSoundFile      string     `yaml:"preSoundFile,omitempty" json:"pre_sound_file,omitempty"`
	PostSoundFile     string     `yaml:"postSoundFile,omitempty" json:"post_sound_file,omitempty"`
	StudyLabel        string     `yaml:"studyLabel,omitempty" json:"study_label,omitempty"`
	ResultsDir        string     `yaml:"resultsDir,omitempty" json:"results_dir,omitempty"`
	Port              string     `yaml:"port,omitempty"`
	Instructions      string     `yaml:"instructions,omitempty" json:"instructions,omitempty"`
}

type soundConfig struct {
	file     string
	streamer beep.StreamSeekCloser
	format   beep.Format
}

type PsychTimer struct {
	config          Config
	conn            *websocket.Conn
	preSound        *soundConfig
	postSound       *soundConfig
	matches         []string
	matchBytes      []byte
	matchMutex      *sync.Mutex
	ch              chan ServerMessage
	currentInterval *Interval
	currentFile     *MindwareFile
	ctx             context.Context
	cancel          context.CancelFunc
	currentPause    *Pause
}

func (p *PsychTimer) maybeShuffleIntervals() {
	if p.config.RandomizeInterval {
		r := rand.New(rand.NewSource(time.Now().UnixNano()))

		r.Shuffle(len(p.config.Intervals), func(i, j int) {
			p.config.Intervals[i], p.config.Intervals[j] = p.config.Intervals[j], p.config.Intervals[i]
		})
	}
}

func loadSoundConfig(file string) *soundConfig {
	log.Debugln("Loading sound file:", file)
	f, err := os.Open(file)
	if err != nil {
		log.Fatal(err)
	}

	streamer, format, err := wav.Decode(f)
	if err != nil {
		log.Fatal(err)
	}

	return &soundConfig{file, streamer, format}

}

func NewPsychTimer(c Config, ch chan ServerMessage) *PsychTimer {
	t, _ := copystructure.Copy(c)
	n := t.(Config)

	for i, interval := range n.Intervals {
		if interval.InputMatcher != "" {
			n.Intervals[i].regexMatcher = regexp.MustCompile(interval.InputMatcher)
		}
	}

	cleanDir, err := filepath.Abs(n.ResultsDir)
	if err != nil {
		log.Fatal(err.Error())
	}
	n.ResultsDir = cleanDir

	log.Debugf("Final config: %+v\n", n)

	cleanPre, err := filepath.Abs(n.PreSoundFile)
	if err != nil {
		log.Fatal(err.Error())
	}
	cleanPost, err := filepath.Abs(n.PostSoundFile)
	if err != nil {
		log.Fatal(err.Error())
	}

	return &PsychTimer{
		config:     n,
		preSound:   loadSoundConfig(cleanPre),
		postSound:  loadSoundConfig(cleanPost),
		ch:         ch,
		matchMutex: &sync.Mutex{},
	}
}

func (p *PsychTimer) SetWSConn(conn *websocket.Conn) error {
	p.conn = conn
	return nil
}

func (p *PsychTimer) playBeep(s *soundConfig) {
	log.Debugf("Playing sound %s\n", s.file)

	s.streamer.Seek(0)
	speaker.Init(s.format.SampleRate, s.format.SampleRate.N(time.Second/10))
	speaker.Play(s.streamer)
}

func cancelableSleep(ctx context.Context, sleep int) (isBreak bool) {
	select {
	case <-ctx.Done():
		isBreak = true
	case <-time.After(time.Duration(sleep) * time.Second):
		isBreak = false
	}
	return
}

func (p *PsychTimer) handlePauses(v Interval, pauses []*Pause) (isBreak bool) {
	defer func() {
		p.currentPause = nil
	}()

	for _, pause := range pauses {
		p.currentPause = pause
		switch pause.Type {
		case "wait":
			// Send message to UI, wait for continue
			pause.wait = make(chan bool, 1)
			p.ch <- ServerMessage{
				Kind:    "WAIT",
				Message: v.Instructions,
			}
			<-pause.wait
		case "time":
			p.ch <- ServerMessage{
				Kind:    "INFO",
				Message: fmt.Sprintf("Interval post wait period: %d seconds", pause.Time),
			}
			p.currentFile.WriteEvent("PsychTimer/"+p.config.StudyLabel+" Event", "Start Pause: "+v.Label)
			isBreak = cancelableSleep(p.ctx, pause.Time)
			if isBreak {
				return
			}
			p.currentFile.WriteEvent("PsychTimer/"+p.config.StudyLabel+" Event", "End Pause: "+v.Label)
		case "input":
			pause.wait = make(chan bool, 1)

			p.ch <- ServerMessage{
				Kind:    "INFO",
				Message: fmt.Sprintf("Waiting for INPUT from subject"),
			}
			log.Debugf("Waiting for input with %+v\n", pause)
			<-pause.wait
		default:
			log.Debugln("Unknown pause condition:", pause.Type)
		}

	}

	return
}

func (p *PsychTimer) RunOne(ID string) {
	p.maybeShuffleIntervals()
	p.ctx, p.cancel = context.WithCancel(context.Background())
	isCanceled := false
	p.ch <- ServerMessage{
		Kind:    "BEGIN",
		Message: ID,
	}
	p.currentFile = NewMindwareFile(filepath.Join(p.config.ResultsDir, ID+".tsv"))
	defer p.currentFile.Close()

	for i, v := range p.config.Intervals {
		p.clearInput()
		p.currentInterval = &p.config.Intervals[i]
		p.ch <- ServerMessage{
			Kind:    "INFO",
			Message: fmt.Sprintf("Starting new interval for %s: %s", ID, v.Label),
		}
		p.currentFile.WriteEvent("PsychTimer/"+p.config.StudyLabel+" Event", "Start Interval: "+v.Label)

		p.handlePauses(v, v.PauseBefore)

		// Interval starts
		if v.PlaySound {
			p.playBeep(p.preSound)
		}
		p.ch <- ServerMessage{
			Kind:    "INFO",
			Message: fmt.Sprintf("Starting interval wait period: %d seconds", v.Time),
		}
		isCanceled = cancelableSleep(p.ctx, v.Time)
		if isCanceled {
			return
		}
		if v.PlaySound {
			p.playBeep(p.postSound)
		}
		p.ch <- ServerMessage{
			Kind:    "INFO",
			Message: fmt.Sprintf("Ending interval wait period: %d seconds", v.Time),
		}

		p.handlePauses(v, v.PauseAfter)
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
	p.matchMutex.Lock()
	defer p.matchMutex.Unlock()
	p.matchBytes = append(p.matchBytes, b)
	log.Debugln(p.matchBytes)
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
			log.Debugf("Matched number with current pause: %+v\n", p.currentPause)
			if p.currentPause != nil && p.currentPause.Type == "input" {
				p.Continue()
			}
		}
	}
}

func (p *PsychTimer) Continue() {
	if p.currentPause.wait != nil {
		p.currentPause.wait <- true
	}
}

func (p *PsychTimer) clearInput() {
	p.matchMutex.Lock()
	defer p.matchMutex.Unlock()

	p.matches = nil
	p.matchBytes = nil
}

func (p *PsychTimer) Conn() *websocket.Conn {
	return p.conn
}

func (p *PsychTimer) Cancel(ID string) {
	p.cancel()
	p.ch <- ServerMessage{
		Kind:    "CANCEL",
		Message: ID,
	}

	p.ch <- ServerMessage{
		Kind:    "END",
		Message: ID,
	}
}
