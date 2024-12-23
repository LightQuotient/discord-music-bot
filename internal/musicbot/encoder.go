package musicbot

import "layeh.com/gopus"

// OpusEncoder is a wrapper around the gopus.Encoder
type OpusEncoder struct {
	encoder *gopus.Encoder
}

// newOpusEncoder constructs a new Gopus encoder set to 48kHz stereo
func newOpusEncoder() (*OpusEncoder, error) {
	enc, err := gopus.NewEncoder(48000, 2, gopus.Audio)
	if err != nil {
		return nil, err
	}
	return &OpusEncoder{encoder: enc}, nil
}

// Encode takes 16-bit PCM data, encodes it to Opus, and returns the encoded bytes
func (oe *OpusEncoder) Encode(pcm []byte) ([]byte, error) {
	// Convert little-endian byte pairs into int16 samples
	pcmData := make([]int16, len(pcm)/2)
	for i := 0; i < len(pcm)/2; i++ {
		pcmData[i] = int16(pcm[2*i]) | int16(pcm[2*i+1])<<8
	}

	// 960 samples at 48 kHz = 20ms of audio
	opusBuf, err := oe.encoder.Encode(pcmData, 960, 4000)
	if err != nil {
		return nil, err
	}
	return opusBuf, nil
}

func (oe *OpusEncoder) Close() {
	// If there's any cleanup needed, do it here
}
