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
	Type         string `yaml:"type,omitempty"`
	Time         int    `yaml:"time,omitempty"` //seconds
	Instructions string `yaml:"instructions,omitempty"`
	wait         chan bool
}

type IntervalGroup struct {
	RandomizeInterval bool       `yaml:"randomizeInterval,omitempty" json:"randomize_interval,omitempty"`
	Intervals         []Interval `yaml:"intervals,omitempty" json:"intervals,omitempty"`
}

type Interval struct {
	Label        string   `yaml:"label,omitempty"`
	Time         int      `yaml:"time,omitempty"` //seconds
	PlaySound    bool     `yaml:"playSound,omitempty"`
	PauseAfter   []*Pause `yaml:"pauseAfter,omitempty"`
	PauseBefore  []*Pause `yaml:"pauseBefore,omitempty"`
	InputMatcher string   `yaml:"inputMatcher,omitempty"`
	CanSkip      bool     `yaml:"canSkip,omitempty"`

	regexMatcher *regexp.Regexp `yaml:"regexMatcher,omitempty"`
}

type Config struct {
	IntervalGroups []IntervalGroup `yaml:"intervalGroups,omitempty" json:"intervalGroups,omitempty"`
	PreSoundFile   string          `yaml:"preSoundFile,omitempty" json:"pre_sound_file,omitempty"`
	PostSoundFile  string          `yaml:"postSoundFile,omitempty" json:"post_sound_file,omitempty"`
	StudyLabel     string          `yaml:"studyLabel,omitempty" json:"study_label,omitempty"`
	ResultsDir     string          `yaml:"resultsDir,omitempty" json:"results_dir,omitempty"`
	Port           string          `yaml:"port,omitempty"`
	Instructions   string          `yaml:"instructions,omitempty" json:"instructions,omitempty"`
}

type soundConfig struct {
	file     string
	streamer beep.StreamSeekCloser
	format   beep.Format
}

type PsychTimer struct {
	config Config
	conn   *websocket.Conn

	// Sound stuff
	preSound         *soundConfig
	postSound        *soundConfig
	soundInitialized bool

	// input matching
	matches         []string
	matchBytes      []byte
	matchMutex      *sync.Mutex
	ch              chan ServerMessage
	currentInterval *Interval
	currentFile     *MindwareFile

	// These variables are the context for the entire run
	runCtx    context.Context
	runCancel context.CancelFunc // Cancel _everything_

	// These variables are the context for the current interval
	intervalCtx    context.Context
	intervalCancel context.CancelFunc // Cancel _everything_
	currentPause   *Pause
}

func (p *PsychTimer) maybeShuffleIntervals(ig IntervalGroup) {
	if ig.RandomizeInterval {
		r := rand.New(rand.NewSource(time.Now().UnixNano()))

		r.Shuffle(len(ig.Intervals), func(i, j int) {
			ig.Intervals[i], ig.Intervals[j] = ig.Intervals[j], ig.Intervals[i]
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

	for j, ig := range n.IntervalGroups {
		for i, interval := range ig.Intervals {
			if interval.InputMatcher != "" {
				n.IntervalGroups[j].Intervals[i].regexMatcher = regexp.MustCompile(interval.InputMatcher)
			}
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
	if !p.soundInitialized {
		speaker.Init(s.format.SampleRate, s.format.SampleRate.N(time.Second/10))
		p.soundInitialized = true
	}
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
			sm := ServerMessage{
				Kind:    "WAIT",
				Message: pause.Instructions,
			}
			if v.CanSkip {
				sm.ExtraInfo = "canSkip"
			}
			p.ch <- sm
			p.currentFile.WriteEvent("PsychTimer/"+p.config.StudyLabel+" Event", "Start Wait Pause: "+v.Label)
			<-pause.wait
			p.currentFile.WriteEvent("PsychTimer/"+p.config.StudyLabel+" Event", "End Wait Pause: "+v.Label)
		case "time":
			p.ch <- ServerMessage{
				Kind:    "INFO",
				Message: fmt.Sprintf("Interval post wait period: %d seconds", pause.Time),
			}
			p.currentFile.WriteEvent("PsychTimer/"+p.config.StudyLabel+" Event", "Start Time Pause: "+v.Label)
			isBreak = cancelableSleep(p.runCtx, pause.Time)
			if isBreak {
				return
			}
			p.currentFile.WriteEvent("PsychTimer/"+p.config.StudyLabel+" Event", "End Time Pause: "+v.Label)
		case "input":
			pause.wait = make(chan bool, 1)

			p.ch <- ServerMessage{
				Kind:    "INFO",
				Message: fmt.Sprintf("Waiting for INPUT from subject"),
			}
			p.currentFile.WriteEvent("PsychTimer/"+p.config.StudyLabel+" Event", "Start Input Pause: "+v.Label)
			log.Debugf("Waiting for input with %+v\n", pause)
			<-pause.wait
			p.currentFile.WriteEvent("PsychTimer/"+p.config.StudyLabel+" Event", "End Input Pause: "+v.Label)
		default:
			log.Debugln("Unknown pause condition:", pause.Type)
		}

	}

	return
}

func (p *PsychTimer) runOneInterval(ID string, g int, i int, v Interval) {
	isCanceled := false
	defer func() {
		p.intervalCancel()
		p.intervalCancel = nil
		p.intervalCtx = nil
	}()

	p.intervalCtx, p.intervalCancel = context.WithCancel(p.runCtx)
	p.clearInput()
	p.currentInterval = &p.config.IntervalGroups[g].Intervals[i]
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
	p.currentFile.WriteEvent("PsychTimer/"+p.config.StudyLabel+" Event", "Start Main Test: "+v.Label)

	isCanceled = cancelableSleep(p.intervalCtx, v.Time)
	p.ch <- ServerMessage{
		Kind:    "INFO",
		Message: fmt.Sprintf("Ending interval wait period: %d seconds", v.Time),
	}
	p.currentFile.WriteEvent("PsychTimer/"+p.config.StudyLabel+" Event", "Stop Main Test: "+v.Label)
	if v.PlaySound {
		p.playBeep(p.postSound)
	}
	if isCanceled {
		return
	}

	p.handlePauses(v, v.PauseAfter)
	p.ch <- ServerMessage{
		Kind:    "INFO",
		Message: fmt.Sprintf("Done with interval for %s: %s", ID, v.Label),
	}
	p.currentFile.WriteEvent("PsychTimer/"+p.config.StudyLabel+" Event", "End Interval: "+v.Label)
}

func (p *PsychTimer) RunOne(ID string) {
	p.runCtx, p.runCancel = context.WithCancel(context.Background())
	p.ch <- ServerMessage{
		Kind:    "BEGIN",
		Message: ID,
	}
	p.currentFile = NewMindwareFile(filepath.Join(p.config.ResultsDir, ID+".txt"))
	defer p.currentFile.Close()

	for j, group := range p.config.IntervalGroups {
		p.maybeShuffleIntervals(group)
		for i, interval := range group.Intervals {
			p.runOneInterval(ID, j, i, interval)
			select {
			case <-p.runCtx.Done():
				// If the entire run was canceled, then exit everything
				return
			case <-time.After(time.Duration(10) * time.Millisecond):
				// do nothing
			}
		}
	}
	p.ch <- ServerMessage{
		Kind:    "END",
		Message: ID,
	}
}

func (p *PsychTimer) AddKey(k string, b byte) {
	p.matchMutex.Lock()
	defer p.matchMutex.Unlock()

	// If we receive an "Enter" we need to pass it along directly,
	// Otherwise we will want to send the key value instead
	if b != 13 {
		p.matchBytes = append(p.matchBytes, []byte(k)[0])
	} else {
		p.matchBytes = append(p.matchBytes, b)
	}
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
	p.runCancel()
	p.currentFile.WriteEvent("PsychTimer/"+p.config.StudyLabel+" Event", "Canceled")

	p.ch <- ServerMessage{
		Kind:    "CANCEL",
		Message: ID,
	}

	p.ch <- ServerMessage{
		Kind:    "END",
		Message: ID,
	}
}

func (p *PsychTimer) Skip(ID string) {
	if p.intervalCancel != nil {
		p.intervalCancel()
	}
	p.currentFile.WriteEvent("PsychTimer/"+p.config.StudyLabel+" Event", "Skip")

	p.ch <- ServerMessage{
		Kind:    "SKIP",
		Message: ID,
	}
}
