package main

import (
	"fmt"
	"html/template"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-chi/chi"
	"github.com/go-chi/chi/middleware"
	"github.com/go-chi/render"
	"github.com/gobuffalo/envy"
	"github.com/jinzhu/gorm"
	_ "github.com/jinzhu/gorm/dialects/postgres"
	"github.com/rs/cors"
)

var db *gorm.DB
var err error

func main() {
	// Load .env file
	envy.Load(".env", "$GOROOT/src/github.com/lasseh/whynoipv6/.env")

	// Database connection
	dsn := fmt.Sprintf("user=%s password=%s dbname=%s host=%s port=%s sslmode=disable",
		envy.Get("V6_USER", ""),
		envy.Get("V6_PASS", ""),
		envy.Get("V6_DB", ""),
		envy.Get("V6_HOST", "localhost"),
		envy.Get("V6_PORT", "5432"),
	)
	db, err = gorm.Open("postgres", dsn)
	if err != nil {
		fmt.Println("Error connecting to database:", err)
	}
	defer db.Close()
	db.LogMode(true)

	// Create a new router
	r := chi.NewRouter()

	// Basic CORS
	cors := cors.New(cors.Options{
		// AllowedOrigins: []string{"https://whynoipv6.com","https://ipv6.fail"}, // Use this to allow specific origin hosts
		AllowedOrigins: []string{"*"},
		// AllowOriginFunc:  func(r *http.Request, origin string) bool { return true },
		AllowedMethods: []string{"GET"},
		//AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-CSRF-Token"},
		ExposedHeaders:   []string{"Link"},
		AllowCredentials: false,
		MaxAge:           300, // Maximum value not ignored by any of major browsers
	})

	// Middleware
	r.Use(
		render.SetContentType(render.ContentTypeJSON), // Set content-Type headers as application/json
		middleware.RealIP,          // Logs the real ip from nginx
		middleware.Logger,          // Log API request calls
		middleware.Recoverer,       // Recover from panics without crashing server
		middleware.RequestID,       // Injects a request ID into the context of each request
		middleware.RedirectSlashes, // Redirect slashes to no slash URL versions
		cors.Handler,               // Handle CORS headers
	)

	// Template function
	funcMap := template.FuncMap{
		"ToLower": strings.ToLower,
	}

	// Template
	tpl := template.New("")
	tpl = template.Must(tpl.Funcs(funcMap).ParseGlob("templates/*"))

	// Index page
	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		data := struct {
			Stats Stats
			Site  []Site
		}{
			getStats(),
			getSites(0, 0),
		}
		tpl.ExecuteTemplate(w, "index", data)
	})

	// About
	r.Get("/about", func(w http.ResponseWriter, r *http.Request) {
		tpl.ExecuteTemplate(w, "about", nil)
	})

	// Country page
	r.Get("/country/{countryCode}", func(w http.ResponseWriter, r *http.Request) {
		countryCode := chi.URLParam(r, "countryCode")

		data := struct {
			Site    []Site
			V6site  []Site
			Country Country
			Stats   Stats
		}{
			getCountry(countryCode),
			getCountryV6(countryCode),
			getCountryName(countryCode),
			getCountryStats(countryCode),
		}

		tpl.ExecuteTemplate(w, "country", data)
	})

	// Stats/Country
	r.Get("/stats/country", func(w http.ResponseWriter, r *http.Request) {
		data := struct {
			CountryStat []CountryStat
		}{
			getCountryStatList(),
		}
		tpl.ExecuteTemplate(w, "stats_country", data)
	})
	// Stats/ASN
	r.Get("/stats/asn", func(w http.ResponseWriter, r *http.Request) {
		queryorder := r.URL.Query().Get("order")
		data := struct {
			Asn []Asn
		}{
			getASNList(queryorder),
		}
		tpl.ExecuteTemplate(w, "stats_asn", data)
	})

	// Serve static web stuff
	workDir, _ := os.Getwd()
	webDir := filepath.Join(workDir, "static")
	fileServer(r, "/static", http.Dir(webDir))

	// API
	r.Mount("/api", apiResource{}.Routes())

	// Start the router
	fmt.Println("Starting server..")
	http.ListenAndServe(":9004", r)
}

// FileServer conveniently sets up a http.FileServer handler to serve
// static files from a http.FileSystem.
func fileServer(r chi.Router, path string, root http.FileSystem) {
	if strings.ContainsAny(path, "{}*") {
		panic("FileServer does not permit URL parameters.")
	}

	fs := http.StripPrefix(path, http.FileServer(root))

	if path != "/" && path[len(path)-1] != '/' {
		r.Get(path, http.RedirectHandler(path+"/", 301).ServeHTTP)
		path += "/"
	}
	path += "*"

	r.Get(path, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fs.ServeHTTP(w, r)
	}))
}
