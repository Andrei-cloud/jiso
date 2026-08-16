package base2

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// CalculateLuhn computes the Modulus-10 Luhn check digit for a numeric string.
func CalculateLuhn(numberStr string) int {
	var sum int
	alternate := true
	for i := len(numberStr) - 1; i >= 0; i-- {
		n, err := strconv.Atoi(string(numberStr[i]))
		if err != nil {
			continue
		}
		if alternate {
			n *= 2
			if n > 9 {
				n = (n % 10) + 1
			}
		}
		sum += n
		alternate = !alternate
	}
	return (10 - (sum % 10)) % 10
}

// GenerateARN synthesizes a valid 23-digit Visa Acquirer Reference Number (ARN).
// Format: 1 digit prefix ('7') + 6-digit BIN + 5-digit Julian date (YYDDD) + 10-digit sequence + 1-digit Mod-10 check digit.
func GenerateARN(bin string, txTime time.Time, seq int64) string {
	cleanBIN := strings.TrimSpace(bin)
	if len(cleanBIN) > 6 {
		cleanBIN = cleanBIN[:6]
	} else if len(cleanBIN) < 6 {
		cleanBIN = fmt.Sprintf("%06s", cleanBIN)
		if strings.TrimSpace(cleanBIN) == "000000" {
			cleanBIN = "400129"
		}
	}

	if txTime.IsZero() {
		txTime = time.Now().UTC()
	}

	// 5-digit Julian date YYDDD
	julianDate := fmt.Sprintf("%02d%03d", txTime.Year()%100, txTime.YearDay())

	// First 22 digits without check digit
	body := fmt.Sprintf("7%s%s%010d", cleanBIN, julianDate, seq%10000000000)
	if len(body) > 22 {
		body = body[:22]
	} else if len(body) < 22 {
		body = fmt.Sprintf("%-22s", body)
	}

	checkDigit := CalculateLuhn(body)
	return fmt.Sprintf("%s%d", body, checkDigit)
}
