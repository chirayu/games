package game

// WordValidator isolates dictionary policy from the game rules.
type WordValidator interface {
	Valid(word string) bool
}

// MVPWordValidator accepts every non-empty word. It remains available only for
// focused domain and transport tests that are unrelated to dictionary content.
type MVPWordValidator struct{}

// Valid implements WordValidator.
func (MVPWordValidator) Valid(word string) bool { return word != "" }
