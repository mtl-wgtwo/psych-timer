package main

import (
	"fmt"
	"log"
	"math/rand"
	"os"
	"time"

	"github.com/docopt/docopt-go"
	"github.com/mitchellh/copystructure"
	"github.com/spf13/viper"

	"github.com/faiface/beep"
	"github.com/faiface/beep/speaker"
	"github.com/faiface/beep/wav"
)

type config struct {
	ConditionInterval []int  `yaml:"conditionInterval,omitempty"`
	RandomizeInterval bool   `yaml:"randomizeInterval,omitempty"`
	PauseInterval     int    `yaml:"pauseInterval,omitempty"`
	PlaySound         bool   `yaml:"playSound,omitempty"`
	SoundFile         string `yaml:"soundFile,omitempty"`
}

type psychTimer struct {
	c config
	s beep.StreamSeekCloser
	f beep.Format
}

func NewPsychTimer(c config) *psychTimer {
	t, _ := copystructure.Copy(c)
	n := t.(config)

	if n.RandomizeInterval {
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

	return &psychTimer{n, streamer, format}
}

func (p *psychTimer) playBeep() {
	fmt.Printf("Playing sound %s\n", p.c.SoundFile)

	p.s.Seek(0)
	speaker.Init(p.f.SampleRate, p.f.SampleRate.N(time.Second/10))
	speaker.Play(p.s)
}

func (p *psychTimer) RunOne() {
	for _, v := range p.c.ConditionInterval {
		fmt.Println("New interval")
		if p.c.PlaySound {
			p.playBeep()
		}
		fmt.Printf("Waiting %d seconds\n", v)
		time.Sleep(time.Duration(v) * time.Second)
		if p.c.PlaySound {
			p.playBeep()
		}
		fmt.Printf("Waiting %d seconds for input\n", p.c.PauseInterval)
		time.Sleep(time.Duration(p.c.PauseInterval) * time.Second)
	}
}

func main() {
	usage := `Psych Timer
	
Usage:
	psych-timer <config>
	
`

	arguments, _ := docopt.ParseDoc(usage)
	fmt.Printf("%+v\n", arguments)

	var c config
	viper.SetConfigName(arguments["<config>"].(string)) // name of config file (without extension)
	viper.AddConfigPath("$HOME/.psych_timer")           // call multiple times to add many search paths
	viper.AddConfigPath(".")                            // optionally look for config in the working directory
	err := viper.ReadInConfig()                         // Find and read the config file
	if err != nil {                                     // Handle errors reading the config file
		panic(fmt.Errorf("Fatal error config file: %s \n", err))
	}

	err = viper.Unmarshal(&c)
	if err != nil {
		fmt.Errorf("unable to decode into struct, %v", err)
	}

	fmt.Printf("%+v\n", c)

	t := NewPsychTimer(c)
	t.RunOne()

}
