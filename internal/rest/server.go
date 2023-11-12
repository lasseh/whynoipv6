package rest

import (
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/render"
	"github.com/rs/cors"
)

// NewRouter instantiates a new router.
func NewRouter() (*chi.Mux, error) {
	r := chi.NewRouter()

	corsMiddleware := cors.New(cors.Options{
		// AllowedOrigins: []string{"https://whynoipv6.com","https://ipv6.fail"}, // Use this to allow specific origin hosts
		AllowedOrigins:   []string{"https://*", "http://*"},
		AllowedMethods:   []string{"GET", "HEAD", "OPTION"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-CSRF-Token"},
		ExposedHeaders:   []string{"Link"},
		AllowCredentials: false,
		MaxAge:           300, // Maximum value not ignored by any of major browsers
	})

	r.Use(
		render.SetContentType(render.ContentTypeJSON), // Set content-Type headers as application/json
		middleware.RealIP,    // Logs the real ip from nginx
		middleware.Logger,    // Log API request calls
		middleware.Recoverer, // Recover from panics without crashing server
		middleware.RequestID, // Injects a request ID into the context of each request
		// middleware.RedirectSlashes, // Redirect slashes to no slash URL versions
		middleware.NoCache, // We dont like cache!
		middleware.SetHeader("X-Content-Type-Options", "nosniff"),
		middleware.SetHeader("X-Frame-Options", "deny"),
		corsMiddleware.Handler,
	)

	return r, nil
}

// PrintRoutes prints the routes of the application.
func PrintRoutes(r *chi.Mux) {
	log.Println("Routes:")
	err := chi.Walk(r, func(method string, route string, handler http.Handler, middlewares ...func(http.Handler) http.Handler) error {
		log.Printf("\t[%s]: '%s'\n", method, route)
		return nil
	})
	if err != nil {
		log.Println("Error printing routes:", err)
	}
	log.Println("")
}

// PaginationInput is the path variables from the request.
type PaginationInput struct {
	Offset int64 `in:"query=offset;default=0"`
	Limit  int64 `in:"query=limit;default=50"`
}
