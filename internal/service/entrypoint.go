package service

/*
Package service is the app glue layer started from main.

It wires settings, login_session refresh loops, and completionApi for
long-lived serve, and can coordinate one-shot flows that need shared
init. cmdHandler calls into this package; it does not parse CLI flags.
*/
