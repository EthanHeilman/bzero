package error

import "encoding/json"

const CurrentVersion = "202205"

type ErrorType string

const (
	// this is any error with the validation of the message itself
	// e.g. invalid signature, expired bzcert, wrong hpointer, etc.
	// The responding actions of any given error type should be the same
	MrtapValidationError       ErrorType = "MrtapValidationError"
	MrtapLegacyValidationError ErrorType = "KeysplittingValidationError"

	// Components such as datachannel, plugin, actions report their actions here.
	// Theoretically, there should be two kinds: any errors that come from
	// startup and any error independent of the message that arises during regular
	// functioning.
	ComponentStartupError    ErrorType = "ComponentStartupError"
	ComponentProcessingError ErrorType = "ComponentProcessingError"
)

type ErrorMessage struct {
	SchemaVersion string    `json:"schemaVersion" default:"202205"`
	Timestamp     int64     `json:"timestamp"`
	Type          ErrorType `json:"type"`
	Message       string    `json:"message"`
	HPointer      string    `json:"hPointer"`
}

// TODO: CWC-2183; remove this logic in the far future
func (et *ErrorType) UnmarshalJSON(data []byte) error {
	var t string
	if err := json.Unmarshal(data, &t); err != nil {
		return err
	}

	if ErrorType(t) == MrtapLegacyValidationError {
		*et = MrtapValidationError
	} else {
		*et = ErrorType(t)
	}

	return nil
}
