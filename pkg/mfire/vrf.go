package mfire

import (
	"container/list"
	"encoding/base64"
	"log"
	"strings"
	"sync"
)

// mustB64 decodes a base64 string; panics if invalid. Used only for
// hardcoded compile-time constants — a panic here means a developer typo.
func mustB64(s string) []byte {
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		log.Panicf("invalid base64 constant: %v", err)
	}
	return b
}

func b64encodeURL(b []byte) string {
	s := base64.StdEncoding.EncodeToString(b)
	s = strings.ReplaceAll(s, "+", "-")
	s = strings.ReplaceAll(s, "/", "_")
	s = strings.TrimRight(s, "=")
	return s
}

// rc4 implements the RC4 stream cipher (KSA + PRGA).
func rc4(key, input []byte) []byte {
	s := make([]int, 256)
	for i := 0; i < 256; i++ {
		s[i] = i
	}
	j := 0
	for i := 0; i < 256; i++ {
		j = (j + s[i] + int(key[i%len(key)])&0xFF) & 0xFF
		s[i], s[j] = s[j], s[i]
	}
	out := make([]byte, len(input))
	i := 0
	j = 0
	for y := 0; y < len(input); y++ {
		i = (i + 1) & 0xFF
		j = (j + s[i]) & 0xFF
		s[i], s[j] = s[j], s[i]
		k := s[(s[i]+s[j])&0xFF]
		out[y] = byte(int(input[y]) ^ k)
	}
	return out
}

func transform(input, initSeed, prefixKey []byte, prefixLen int, schedule []func(int) int) []byte {
	out := make([]byte, 0, len(input)+prefixLen)
	for i := 0; i < len(input); i++ {
		if i < prefixLen {
			out = append(out, prefixKey[i])
		}
		t := schedule[i%10]((int(input[i])^int(initSeed[i%32]))&0xFF) & 0xFF
		out = append(out, byte(t))
	}
	return out
}

// ------- schedules -------

func scheduleC() []func(int) int {
	return []func(int) int{
		func(c int) int { return (c - 48 + 256) & 0xFF },
		func(c int) int { return (c - 19 + 256) & 0xFF },
		func(c int) int { return (c ^ 241) & 0xFF },
		func(c int) int { return (c - 19 + 256) & 0xFF },
		func(c int) int { return (c + 223) & 0xFF },
		func(c int) int { return (c - 19 + 256) & 0xFF },
		func(c int) int { return (c - 170 + 256) & 0xFF },
		func(c int) int { return (c - 19 + 256) & 0xFF },
		func(c int) int { return (c - 48 + 256) & 0xFF },
		func(c int) int { return (c ^ 8) & 0xFF },
	}
}

func scheduleY() []func(int) int {
	return []func(int) int{
		func(c int) int { return ((c << 4) | (c >> 4)) & 0xFF },
		func(c int) int { return (c + 223) & 0xFF },
		func(c int) int { return ((c << 4) | (c >> 4)) & 0xFF },
		func(c int) int { return (c ^ 163) & 0xFF },
		func(c int) int { return (c - 48 + 256) & 0xFF },
		func(c int) int { return (c + 82) & 0xFF },
		func(c int) int { return (c + 223) & 0xFF },
		func(c int) int { return (c - 48 + 256) & 0xFF },
		func(c int) int { return (c ^ 83) & 0xFF },
		func(c int) int { return ((c << 4) | (c >> 4)) & 0xFF },
	}
}

func scheduleB() []func(int) int {
	return []func(int) int{
		func(c int) int { return (c - 19 + 256) & 0xFF },
		func(c int) int { return (c + 82) & 0xFF },
		func(c int) int { return (c - 48 + 256) & 0xFF },
		func(c int) int { return (c - 170 + 256) & 0xFF },
		func(c int) int { return ((c << 4) | (c >> 4)) & 0xFF },
		func(c int) int { return (c - 48 + 256) & 0xFF },
		func(c int) int { return (c - 170 + 256) & 0xFF },
		func(c int) int { return (c ^ 8) & 0xFF },
		func(c int) int { return (c + 82) & 0xFF },
		func(c int) int { return (c ^ 163) & 0xFF },
	}
}

func scheduleJ() []func(int) int {
	return []func(int) int{
		func(c int) int { return (c + 223) & 0xFF },
		func(c int) int { return ((c << 4) | (c >> 4)) & 0xFF },
		func(c int) int { return (c + 223) & 0xFF },
		func(c int) int { return (c ^ 83) & 0xFF },
		func(c int) int { return (c - 19 + 256) & 0xFF },
		func(c int) int { return (c + 223) & 0xFF },
		func(c int) int { return (c - 170 + 256) & 0xFF },
		func(c int) int { return (c + 223) & 0xFF },
		func(c int) int { return (c - 170 + 256) & 0xFF },
		func(c int) int { return (c ^ 83) & 0xFF },
	}
}

func scheduleE() []func(int) int {
	return []func(int) int{
		func(c int) int { return (c + 82) & 0xFF },
		func(c int) int { return (c ^ 83) & 0xFF },
		func(c int) int { return (c ^ 163) & 0xFF },
		func(c int) int { return (c + 82) & 0xFF },
		func(c int) int { return (c - 170 + 256) & 0xFF },
		func(c int) int { return (c ^ 8) & 0xFF },
		func(c int) int { return (c ^ 241) & 0xFF },
		func(c int) int { return (c + 82) & 0xFF },
		func(c int) int { return (c + 176) & 0xFF },
		func(c int) int { return ((c << 4) | (c >> 4)) & 0xFF },
	}
}

// ------- static key material -------

var rc4Keys = map[string]string{
	"l": "u8cBwTi1CM4XE3BkwG5Ble3AxWgnhKiXD9Cr279yNW0=",
	"g": "t00NOJ/Fl3wZtez1xU6/YvcWDoXzjrDHJLL2r/IWgcY=",
	"B": "S7I+968ZY4Fo3sLVNH/ExCNq7gjuOHjSRgSqh6SsPJc=",
	"m": "7D4Q8i8dApRj6UWxXbIBEa1UqvjI+8W0UvPH9talJK8=",
	"F": "0JsmfWZA1kwZeWLk5gfV5g41lwLL72wHbam5ZPfnOVE=",
}

var seeds32 = map[string]string{
	"A": "pGjzSCtS4izckNAOhrY5unJnO2E1VbrU+tXRYG24vTo=",
	"V": "dFcKX9Qpu7mt/AD6mb1QF4w+KqHTKmdiqp7penubAKI=",
	"N": "owp1QIY/kBiRWrRn9TLN2CdZsLeejzHhfJwdiQMjg3w=",
	"P": "H1XbRvXOvZAhyyPaO68vgIUgdAHn68Y6mrwkpIpEue8=",
	"k": "2Nmobf/mpQ7+Dxq1/olPSDj3xV8PZkPbKaucJvVckL0=",
}

var prefixKeys = map[string]string{
	"O": "Rowe+rg/0g==",
	"v": "8cULcnOMJVY8AA==",
	"L": "n2+Og2Gth8Hh",
	"p": "aRpvzH+yoA==",
	"W": "ZB4oBi0=",
}

// ------- VRF LRU cache -------

type vrfCache struct {
	mu       sync.Mutex
	ll       *list.List
	entries  map[string]*list.Element
	capacity int
}

type vrfEntry struct {
	key   string
	value string
}

func newVRFCache(capacity int) *vrfCache {
	return &vrfCache{
		ll:       list.New(),
		entries:  make(map[string]*list.Element, capacity),
		capacity: capacity,
	}
}

func (c *vrfCache) getOrStore(key string, fn func() (string, error)) (string, error) {
	c.mu.Lock()
	if el, ok := c.entries[key]; ok {
		c.ll.MoveToFront(el)
		v := el.Value.(*vrfEntry).value
		c.mu.Unlock()
		return v, nil
	}
	c.mu.Unlock()

	val, err := fn()
	if err != nil {
		return "", err
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.entries[key]; ok {
		c.ll.MoveToFront(el)
		return el.Value.(*vrfEntry).value, nil
	}
	ent := &vrfEntry{key: key, value: val}
	el := c.ll.PushFront(ent)
	c.entries[key] = el
	if c.ll.Len() > c.capacity {
		back := c.ll.Back()
		if back != nil {
			c.ll.Remove(back)
			delete(c.entries, back.Value.(*vrfEntry).key)
		}
	}
	return val, nil
}

var (
	globalVRFCache = newVRFCache(1024)
)

// generateVRF computes the VRF token for a given input string without caching.
func generateVRF(input string) (string, error) {
	bytes := []byte(input)

	k1 := mustB64(rc4Keys["l"])
	bytes = rc4(k1, bytes)

	seedA := mustB64(seeds32["A"])
	prefO := mustB64(prefixKeys["O"])
	bytes = transform(bytes, seedA, prefO, 7, scheduleC())

	k2 := mustB64(rc4Keys["g"])
	bytes = rc4(k2, bytes)

	seedV := mustB64(seeds32["V"])
	prefV := mustB64(prefixKeys["v"])
	bytes = transform(bytes, seedV, prefV, 10, scheduleY())

	k3 := mustB64(rc4Keys["B"])
	bytes = rc4(k3, bytes)

	seedN := mustB64(seeds32["N"])
	prefL := mustB64(prefixKeys["L"])
	bytes = transform(bytes, seedN, prefL, 9, scheduleB())

	k4 := mustB64(rc4Keys["m"])
	bytes = rc4(k4, bytes)

	seedP := mustB64(seeds32["P"])
	prefP := mustB64(prefixKeys["p"])
	bytes = transform(bytes, seedP, prefP, 7, scheduleJ())

	k5 := mustB64(rc4Keys["F"])
	bytes = rc4(k5, bytes)

	seedK := mustB64(seeds32["k"])
	prefW := mustB64(prefixKeys["W"])
	bytes = transform(bytes, seedK, prefW, 5, scheduleE())

	return b64encodeURL(bytes), nil
}

// VRF returns a VRF token for the given query string. Results are cached in
// an LRU cache to avoid recomputing identical queries.
func VRF(input string) (string, error) {
	return globalVRFCache.getOrStore(input, func() (string, error) {
		return generateVRF(input)
	})
}

// PurgeVRFCache clears the VRF token cache. Useful when tokens expire or
// when the caller wants fresh tokens after a long runtime.
func PurgeVRFCache() {
	globalVRFCache.mu.Lock()
	defer globalVRFCache.mu.Unlock()
	globalVRFCache.ll = list.New()
	globalVRFCache.entries = make(map[string]*list.Element, globalVRFCache.capacity)
}
