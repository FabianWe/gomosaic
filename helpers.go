package gomosaic

func MaxUint32(a uint32, elements ...uint32) uint32 {
	res := a
	for _, val := range elements {
		if val > res {
			res = val
		}
	}
	return res
}

func MinUint32(a uint32, elements ...uint32) uint32 {
	res := a
	for _, val := range elements {
		if val < res {
			res = val
		}
	}
	return res
}

func MaxUint8(a uint8, elements ...uint8) uint8 {
	res := a
	for _, val := range elements {
		if val > res {
			res = val
		}
	}
	return res
}

func MinUint8(a uint8, elements ...uint8) uint8 {
	res := a
	for _, val := range elements {
		if val < res {
			res = val
		}
	}
	return res
}
