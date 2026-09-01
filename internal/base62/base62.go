package base62

const alphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

func Encode(num int64) string {
	if num == 0 {
		return "0"
	}
	buf := make([]byte, 0, 11)
	for num > 0 {
		rem := num % 62
		buf = append([]byte{alphabet[rem]}, buf...)
		num /= 62
	}
	return string(buf)
}

func Decode(s string) (int64, bool) {
	var n int64
	for i := 0; i < len(s); i++ {
		idx := int64(-1)
		for j := 0; j < len(alphabet); j++ {
			if alphabet[j] == s[i] {
				idx = int64(j)
				break
			}
		}
		if idx < 0 {
			return 0, false
		}
		n = n*62 + idx
	}
	return n, true
}
