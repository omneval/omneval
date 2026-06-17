package auth

// ProjectIDContextKey is the context key for the project ID.
var ProjectIDContextKey any = "omneval_project_id"

// UserIDContextKey is the context key for the authenticated user's ID.
var UserIDContextKey any = "omneval_user_id"

// EmailContextKey is the context key for the authenticated user's email.
var EmailContextKey any = "omneval_email"

// AdminEmailContextKey is the context key for the admin email.
var AdminEmailContextKey any = "omneval_admin_email"

// APIKeyProjectIDContextKey is the context key under which the middleware
// stores the project ID derived from a validated API key.
var APIKeyProjectIDContextKey any = "omneval_api_key_project_id"
