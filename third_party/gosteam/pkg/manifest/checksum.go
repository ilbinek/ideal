package manifest

// Adler32 computes the Adler-32 checksum the way SteamKit2 does, seeding the
// accumulator with 0 (RFC1950 seeds s1 with 1). Steam stores this value as each
// chunk's CRC, so chunk verification and delta reuse must match it exactly.
// Ported from SteamKit2's Util/Adler32.cs (Calculate(0, data)).
func Adler32(data []byte) uint32 {
	const (
		base = 65521 // largest prime < 65536
		nmax = 5552  // max bytes before s2 can overflow uint32
	)
	var s1, s2 uint32 // seeded with 0, not 1

	for len(data) > 0 {
		k := len(data)
		if k > nmax {
			k = nmax
		}
		for _, b := range data[:k] {
			s1 += uint32(b)
			s2 += s1
		}
		s1 %= base
		s2 %= base
		data = data[k:]
	}
	return s2<<16 | s1
}
