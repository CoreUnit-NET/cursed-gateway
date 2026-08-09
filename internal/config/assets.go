package config

/*
Env helpers used while building AppConfig (string / int / bool).
*/

import (
	"errors"
	"fmt"
	"os"
	"strconv"
)

func envIsString(envVar string, existCallback func(value string)) error {
	value := os.Getenv(envVar)
	if len(value) == 0 {
		return nil
	}

	existCallback(value)
	return nil
}

func envIsInt(envVar string, existCallback func(value int)) error {
	value := os.Getenv(envVar)
	if len(value) == 0 {
		return nil
	}

	intValue, err := strconv.Atoi(value)
	if err != nil {
		return fmt.Errorf("invalid %s value %q: want integer", envVar, value)
	}

	existCallback(intValue)
	return nil
}

func envIsBool(envVar string, existCallback func(value bool)) error {
	value := os.Getenv(envVar)
	if len(value) == 0 {
		return nil
	}

	boolValue, err := strconv.ParseBool(value)
	if err != nil {
		return fmt.Errorf("invalid %s value %q: want boolean", envVar, value)
	}

	existCallback(boolValue)
	return nil
}

// ErrHelpRequested is returned when the user asked for command help.
// Callers should exit successfully after cobra has printed help.
var ErrHelpRequested = errors.New("help requested")
