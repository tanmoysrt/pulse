package main

import (
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"runtime"
)

// sound is one chime across both platforms: a canberra event id (+dB volume)
// and .oga fallback for Linux, and a system sound name for macOS.
type sound struct {
	linuxID   string
	linuxFile string
	linuxDB   string
	macName   string
}

var (
	soundDone  = sound{"complete", "complete.oga", "-6", "Glass"}
	soundAlert = sound{"dialog-warning", "dialog-warning.oga", "-3", "Funk"}
)

var cuteDoneMessages = []string{
	"%s finished, come see what it did! 🎉",
	"All done! %s is waiting for you ✨",
	"%s wrapped up. Your turn! 🌸",
	"Ding! %s just finished cooking 🍳",
	"Task complete, %s says hi 👋",
	"%s is done and looking rather pleased with itself 😌",
}

func (s *Server) agentName() string {
	name := s.agent
	if name != "" {
		name = string(name[0]-'a'+'A') + name[1:]
	}
	return name
}

// notifyDone announces the agent finished its turn.
func (s *Server) notifyDone() {
	if s.quiet {
		return
	}
	body := fmt.Sprintf(cuteDoneMessages[rand.Intn(len(cuteDoneMessages))], s.agentName())
	go notify("✨ pulse", body, soundDone)
	go s.pushAll("✨ pulse", body)
}

// notifyPermission announces the agent is waiting for tool approval.
func (s *Server) notifyPermission(tool string) {
	if s.quiet {
		return
	}
	body := fmt.Sprintf("%s wants to run %s. approve? 🔐", s.agentName(), tool)
	if tool == "" {
		body = fmt.Sprintf("%s needs your permission 🔐", s.agentName())
	}
	go notify("🔐 pulse: needs you", body, soundAlert)
	go s.pushAll("🔐 pulse: needs you", body)
}

// notify pops a native OS notification on Linux and macOS. Best-effort: any
// failure is ignored so it never disrupts the session.
func notify(title, body string, snd sound) {
	switch runtime.GOOS {
	case "darwin":
		script := fmt.Sprintf("display notification %q with title %q sound name %q", body, title, snd.macName)
		_ = exec.Command("osascript", "-e", script).Start()
	case "linux":
		_ = exec.Command("notify-send", "-a", "pulse", "-u", "normal", title, body).Start()
		playSound(snd) // notify-send is silent on its own
	}
}

// playSound plays snd's chime on Linux, trying each available player in turn.
func playSound(snd sound) {
	if p, err := exec.LookPath("canberra-gtk-play"); err == nil {
		_ = exec.Command(p, "-i", snd.linuxID, "-V", snd.linuxDB).Start()
		return
	}
	file := "/usr/share/sounds/freedesktop/stereo/" + snd.linuxFile
	if _, err := os.Stat(file); err != nil {
		return
	}
	for _, player := range []string{"paplay", "pw-play", "ogg123"} {
		if p, err := exec.LookPath(player); err == nil {
			args := []string{file}
			if player == "paplay" {
				args = []string{"--volume=33000", file} // ~-6dB
			}
			_ = exec.Command(p, args...).Start()
			return
		}
	}
}
