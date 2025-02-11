package seed

type Seeder struct {
	lowers []string
	uppers []string
	nums   []string
}

func RegisterUrlSeeder() *Seeder {
	var lowercase = []string{
		"a", "b", "c", "d", "e",
		"f", "g", "h", "j",
		"k", "m", "n",
		"p", "q", "r", "s", "t",
		"u", "v", "w", "x", "y", "z",
	}

	var numbers = []string{
		"1", "2", "3", "4", "5",
		"6", "7", "8", "9",
	}

	var uppercase = []string{
		"A", "B", "C", "D", "E",
		"F", "G", "H", "J",
		"K", "M", "N",
		"P", "Q", "R", "S", "T",
		"U", "V", "W", "X", "Y", "Z",
	}

	return &Seeder{
		lowers: lowercase,
		uppers: uppercase,
		nums:   numbers,
	}
}

func (seeder *Seeder) Lowers() []string {
	return seeder.lowers
}

func (seeder *Seeder) Shards(batchSize int) []string {
	// TODO: this should be a config
	return []string{
		"a-e",
		"f-j",
		"k-p",
		"q-u",
		"v-z",
	}
}
