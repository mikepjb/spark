package llm

type connection struct {
	host string
	port int
}

func (c connection) Send() {
	// sends a message, response determines whether it was a success, queued
	// (still success but diff state) or a failure.
}
