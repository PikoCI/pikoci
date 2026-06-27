package pikoci

type contextKey string

// ActorContextKey is the context key used to pass the authenticated username
// to the service layer for audit logging.
const ActorContextKey contextKey = "pikoci_actor"
