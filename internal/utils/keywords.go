// keywords.go names the dynamic-field keywords: values a spec, a scenario, or a
// generated scenario writes into a field to mean "fill this in when the message is
// composed", as opposed to a literal the operator typed.
//
// Three layers must spell them identically -- the analyzer generates them, the
// composer expands them, and the mock route engine substitutes them -- and the
// failure mode of a mismatch is visible on the wire: a keyword the composer does
// not recognise is sent to the peer as the text "auto".
package utils

const (
	// KeywordAuto means "derive this field at compose time" -- the timestamp,
	// STAN, RRN and retrieval reference the composer owns.
	KeywordAuto = "auto"
	// KeywordRandom draws a value from the dataset row instead of deriving one,
	// which is why the composer treats it as a lookup rather than a generator.
	KeywordRandom = "random"
	// KeywordAuthCode is the authorisation code the composer draws from the
	// dataset row rather than inventing.
	KeywordAuthCode = "auth_code"
	// KeywordSTAN makes the responder generate a fresh sequence number instead of
	// repeating the one it captured, so two runs of the same mock route are two
	// different transactions to the peer.
	KeywordSTAN = "stan"
	// KeywordRRN is the retrieval reference number the responder generates, for
	// the same reason as KeywordSTAN.
	KeywordRRN = "rrn"
	// KeywordDateTime makes the responder stamp the message now rather than at the
	// time the capture was taken.
	KeywordDateTime = "datetime"
)
