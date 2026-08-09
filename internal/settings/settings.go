package settings

/*
Package settings turns raw config into a validated, app-ready Settings
struct.

Convert/parse values into usable types (including atomics if needed).
Every field must have a validator that runs before Settings is returned.
Session token persistence stays in login_session; this package only
exposes paths and runtime options from config (e.g. AUTH_PATH / data dir).
*/
