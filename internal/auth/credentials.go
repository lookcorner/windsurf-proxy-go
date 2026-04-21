// Package auth provides credential discovery and authentication for Windsurf.
package auth

// WindsurfCredentials holds credentials needed to talk to a Windsurf language server.
type WindsurfCredentials struct {
	CSRFToken string
	GRPCPort  int
	APIKey    string
	Version   string
}

// FirebaseTokens holds tokens obtained from Firebase sign-in.
type FirebaseTokens struct {
	IDToken      string
	RefreshToken string
	ExpiresIn    int // seconds
	Email        string
}

// WindsurfServiceAuth holds service credentials obtained from RegisterUser.
type WindsurfServiceAuth struct {
	APIKey        string
	Name          string
	APIServerURL  string
}

// Constants for Firebase and Windsurf authentication
const (
	FirebaseAPIKey     = "AIzaSyDsOl-1XpT5err0Tcnx8FFod1H8gVGIycY"
	FirebaseProjectID  = "exa2-fb170"
	FirebaseAppID      = "1:957777847521:web:390f31e87633dc5cc803a0"

	// Firebase endpoints
	FirebaseSignInURL  = "https://identitytoolkit.googleapis.com/v1/accounts:signInWithPassword?key=" + FirebaseAPIKey
	FirebaseRefreshURL = "https://securetoken.googleapis.com/v1/token?key=" + FirebaseAPIKey

	// Windsurf endpoints
	RegisterUserURL    = "https://register.windsurf.com/exa.seat_management_pb.SeatManagementService/RegisterUser"
	DefaultAPIServerURL = "https://server.self-serve.windsurf.com"
)