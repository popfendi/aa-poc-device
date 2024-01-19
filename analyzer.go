package main

import (
	"encoding/json"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"time"

	"github.com/gordonklaus/portaudio"
	"github.com/mjibson/go-dsp/spectral"
)

var buffer []float32
var mu sync.Mutex

func analyze(dataChannel chan<- []byte) {
	processAudio := func(in []float32) {
		mu.Lock()
		defer mu.Unlock()

		// Append captured audio to buffer
		buffer = append(buffer, in...)

		const sampleRate = 44100
		const fftSize = 1024
		const bufferSize = 22050

		// Ensure we have enough data before proceeding
		if len(buffer) < bufferSize {
			return
		}

		// Convert to float64 for spectral analysis
		data := make([]float64, len(buffer))
		for i, v := range buffer {
			data[i] = float64(v)
		}

		// Calculate power spectral density
		psd, _ := spectral.Pwelch(data, sampleRate, &spectral.PwelchOptions{NFFT: fftSize})

		output := make(map[string]interface{})

		ts := strconv.FormatInt(time.Now().UnixMilli(), 10)

		output["spectrum"] = psd
		output["ts"] = ts

		// Convert output to JSON
		jsonOutput, err := json.Marshal(output)
		if err != nil {
			Sugar.Errorf("Error converting output to JSON: %s", err.Error())
			return
		}

		dataChannel <- jsonOutput

		// Clear the buffer
		buffer = nil
	}

	// Initialize PortAudio
	portaudio.Initialize()
	defer portaudio.Terminate()

	// Set up microphone input stream
	stream, err := portaudio.OpenDefaultStream(1, 0, 44100, 1024, processAudio)
	if err != nil {
		Sugar.Fatalf("Failed to open audio stream: %s", err.Error())
	}
	defer stream.Close()

	// Start capturing audio
	if err := stream.Start(); err != nil {
		Sugar.Fatalf("Failed to start audio stream: %s", err.Error())
	}
	defer stream.Stop()

	Sugar.Info("Capturing audio...")

	// Wait for Ctrl+C
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)
	<-sig

	Sugar.Info("Exiting with code 1")
}
