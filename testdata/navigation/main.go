package navigationfixture

type Speaker interface {
	Speak() string
}

type Bird struct{}

func (Bird) Speak() string { return "hello" }

func Greet(s Speaker) string { return s.Speak() }

func Example() string { return Greet(Bird{}) }
