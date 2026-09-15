package result

import (
	"math/big"
	"strconv"
	"strings"
)

// Decimal is an exact number, Coef × 10^-Scale: a warehouse decimal or a uint64 past int64.
type Decimal struct {
	Coef  *big.Int
	Scale int32 // never negative
}

// newDecimal folds a negative scale into Coef, and returns int64 when the value fits one.
func newDecimal(coef *big.Int, scale int32) any {
	if scale < 0 {
		coef = new(big.Int).Mul(coef, new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(-scale)), nil))
		scale = 0
	}
	if scale == 0 && coef.IsInt64() {
		return coef.Int64()
	}
	return Decimal{Coef: coef, Scale: scale}
}

func (d Decimal) String() string {
	digits := d.Coef.String()
	sign := ""
	if strings.HasPrefix(digits, "-") {
		sign, digits = "-", digits[1:]
	}
	scale := int(d.Scale)
	if scale == 0 {
		return sign + digits
	}
	if len(digits) <= scale {
		digits = strings.Repeat("0", scale-len(digits)+1) + digits
	}
	return sign + digits[:len(digits)-scale] + "." + digits[len(digits)-scale:]
}

func (d Decimal) Float64() float64 {
	f, _ := strconv.ParseFloat(d.String(), 64)
	return f
}
