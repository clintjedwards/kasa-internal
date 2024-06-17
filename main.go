package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/clintjedwards/innerhaven/internal/config"
	"github.com/clintjedwards/innerhaven/internal/frontend"
	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/rs/zerolog/log"
)

func ptr[T any](v T) *T {
	return &v
}

type APIContext struct {
	config *config.API
}

// NewAPI creates a new instance of the main Gofer API service.
func NewAPI(config *config.API) (*APIContext, error) {
	newAPI := &APIContext{
		config: config,
	}

	return newAPI, nil
}

// cleanup gracefully cleans up all goroutines to ensure a clean shutdown.
func (apictx *APIContext) cleanup() {
}

// StartAPIService starts the Gofer API service and blocks until a SIGINT or SIGTERM is received.
func (apictx *APIContext) StartAPIService() {
	tlsConfig, err := apictx.generateTLSConfig(apictx.config.Server.TLSCertPath, apictx.config.Server.TLSKeyPath)
	if err != nil {
		log.Fatal().Err(err).Msg("could not get proper TLS config")
	}

	// Assign all routes and handlers
	router, _ := InitRouter(apictx)

	httpServer := http.Server{
		Addr:         apictx.config.Server.ListenAddress,
		Handler:      loggingMiddleware(router),
		WriteTimeout: apictx.config.Server.WriteTimeout,
		ReadTimeout:  apictx.config.Server.ReadTimeout,
		IdleTimeout:  apictx.config.Server.IdleTimeout,
		TLSConfig:    tlsConfig,
	}

	// Run our server in a goroutine and listen for signals that indicate graceful shutdown
	go func() {
		if err := httpServer.ListenAndServeTLS("", ""); err != nil && err != http.ErrServerClosed {
			log.Fatal().Err(err).Msg("server exited abnormally")
		}
	}()
	log.Info().Str("url", apictx.config.Server.ListenAddress).Msg("started gofer http service")

	c := make(chan os.Signal, 1)
	signal.Notify(c, syscall.SIGTERM, syscall.SIGINT)
	<-c

	// On ctrl-c we need to clean up not only the connections from the server, but make sure all the currently
	// running jobs are logged and exited properly.
	apictx.cleanup()

	// Doesn't block if no connections, otherwise will wait until the timeout deadline or connections to finish,
	// whichever comes first.
	ctx, cancel := context.WithTimeout(context.Background(), apictx.config.Server.ShutdownTimeout) // shutdown gracefully
	defer cancel()

	err = httpServer.Shutdown(ctx)
	if err != nil {
		log.Error().Err(err).Msg("could not shutdown server in timeout specified")
		return
	}

	log.Info().Msg("http server exited gracefully")
}

// The logging middleware has to be run before the final call to return the request.
// This is because we wrap the responseWriter to gain information from it after it
// has been written to (this enables us to get things that we only know after the request has been served like status codes).
// To speed this process up we call Serve as soon as possible and log afterwards.
func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(ww, r)

		log.Debug().Str("method", r.Method).
			Stringer("url", r.URL).
			Int("status_code", ww.Status()).
			Int("response_size_bytes", ww.BytesWritten()).
			Float64("elapsed_ms", float64(time.Since(start))/float64(time.Millisecond)).
			Msg("")
	})
}

// Create a new http router that gets populated by huma lib. Huma helps create an OpenAPI spec and documentation
// from REST code. We export this function so that we can use it in external scripts to generate the OpenAPI spec
// for this API in other places.
func InitRouter(apictx *APIContext) (router *http.ServeMux, apiDescription huma.API) {
	router = http.NewServeMux()

	version, _ := parseVersion(appVersion)
	humaConfig := huma.DefaultConfig("Gofer", version)
	humaConfig.Info.Description = "Gofer is an opinionated, streamlined automation engine designed for the cloud-native " +
		"era. It specializes in executing your custom scripts in a containerized environment, making it versatile for " +
		"both developers and operations teams. Deploy Gofer effortlessly as a single static binary, and " +
		"manage it using expressive, declarative configurations written in real programming languages. Once " +
		"set up, Gofer takes care of scheduling and running your automation tasks—be it on Nomad, Kubernetes, or even Local Docker." +
		"\n" +
		"Its primary function is to execute short-term jobs like code linting, build automation, testing, port scanning, " +
		"ETL operations, or any task you can containerize and trigger based on events."

	humaConfig.DocsPath = "/api/docs"
	humaConfig.OpenAPIPath = "/api/docs/openapi"
	humaConfig.Servers = append(humaConfig.Servers, &huma.Server{
		URL: apictx.config.Server.ListenAddress,
	})
	humaConfig.Components.SecuritySchemes = map[string]*huma.SecurityScheme{
		"bearer": {
			Type:   "http",
			Scheme: "bearer",
		},
	}

	apiDescription = humago.New(router, humaConfig)

	/* /api/system */
	apictx.registerDescribeSystemInfo(apiDescription)
	apictx.registerDescribeSystemSummary(apiDescription)

	/* /api/lights */
	// apictx.registerCreateToken(apiDescription)

	// /* /api/weather */
	// apictx.registerDescribeTaskExecution(apiDescription)

	// /* /api/transit */
	// apictx.registerDescribeTaskExecution(apiDescription)

	// Set up the frontend paths last since they capture everything that isn't in the API path.
	if apictx.config.Development.LoadFrontendFilesFromDisk {
		log.Warn().Msg("Loading frontend files from local disk dir 'public'; Not for use in production.")
		router.Handle("/", frontend.LocalHandler())
	} else {
		router.Handle("/", frontend.StaticHandler())
	}

	if apictx.config.Development.GenerateOpenAPISpecFiles {
		generateOpenAPIFiles(apiDescription)
	}

	return router, apiDescription
}

// Generates OpenAPI Yaml files that other services can use to generate code for Gofer's API.
func generateOpenAPIFiles(apiDescription huma.API) {
	output, err := apiDescription.OpenAPI().YAML()
	if err != nil {
		panic(err)
	}

	file, err := os.Create("openapi.yaml")
	if err != nil {
		panic(err)
	}
	defer file.Close()

	_, err = file.Write(output)
	if err != nil {
		panic(err)
	}
}
