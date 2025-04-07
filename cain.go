package main

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/kelseyhightower/envconfig"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/common/version"
	kwhhttp "github.com/slok/kubewebhook/v2/pkg/http"
	kwhprometheus "github.com/slok/kubewebhook/v2/pkg/metrics/prometheus"
	kwhwebhook "github.com/slok/kubewebhook/v2/pkg/webhook"
	kwhmutating "github.com/slok/kubewebhook/v2/pkg/webhook/mutating"
	kwhvalidating "github.com/slok/kubewebhook/v2/pkg/webhook/validating"
	"github.com/sourcegraph/conc/pool"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"github.com/weisshorn-cyd/cain/certificates"
	localhttp "github.com/weisshorn-cyd/cain/http"
	"github.com/weisshorn-cyd/cain/metadata"
	"github.com/weisshorn-cyd/cain/metrics"
	"github.com/weisshorn-cyd/cain/secrets"
	"github.com/weisshorn-cyd/cain/utils"
	"github.com/weisshorn-cyd/cain/webhook"
)

type envConfig struct {
	Port               string          `default:"8443"                                                                                    desc:"The webhook HTTPS port"                                                                      envconfig:"PORT"`
	MetricsPort        string          `default:"8080"                                                                                    desc:"The metrics HTTP port"                                                                       envconfig:"METRICS_PORT"`
	LogLevel           *slog.LevelVar  `default:"info"                                                                                    desc:"The level to log at"                                                                         envconfig:"LOG_LEVEL"`
	TLSCertFile        string          `default:"/run/secrets/tls/tls.crt"                                                                desc:"Path to the file containing the TLS Certificate"                                             envconfig:"TLS_CERT_FILE"`
	TLSKeyFile         string          `default:"/run/secrets/tls/tls.key"                                                                desc:"Path to the file containing the TLS Key"                                                     envconfig:"TLS_KEY_FILE"`
	MetadataDomain     string          `default:"weisshorn.cyd"                                                                           desc:"The domain of the labels and annotations, this can allow multiple instances of the injector" envconfig:"METADATA_DOMAIN"`
	CAIssuer           string          `desc:"The CA issuer to use when creating Certificate resources"                                   envconfig:"CA_ISSUER"                                                                              required:"true"`
	CASecret           *utils.CASecret `desc:"The default CA secret to use, with the key of the CA, <secret name>/<CA key>[,<CA key>...]" envconfig:"CA_SECRET"                                                                              required:"true"`
	TruststorePassword string          `desc:"The password to use for the JVM truststore"                                                 envconfig:"TRUSTSTORE_PASSWORD"                                                                    required:"true"`
	JVMEnvVariable     string          `desc:"The ENV variable to use for JVM containers"                                                 envconfig:"JVM_ENV_VAR"                                                                            required:"true"`
	RedHatInitImage    string          `default:"ghcr.io/weisshorn-cyd/cain-redhat-init"                                                  desc:"The container image to use for the RedHat family init containers"                            envconfig:"REDHAT_INIT_IMAGE"`
	RedHatInitTag      string          `desc:"The container image tag to use for the RedHat family init containers"                       envconfig:"REDHAT_INIT_TAG"`
	DebianInitImage    string          `default:"ghcr.io/weisshorn-cyd/cain-debian-init"                                                  desc:"The container image to use for the Debian family init containers"                            envconfig:"DEBIAN_INIT_IMAGE"`
	DebianInitTag      string          `desc:"The container image tag to use for the Debian family init containers"                       envconfig:"DEBIAN_INIT_TAG"`
	MetricsSubsystem   string          `default:""                                                                                        desc:"The subsystem for the metrics"                                                               envconfig:"METRICS_SUBSYSTEM"`

	webhook.ContainerResourcesEnv
}

var ErrSecretKeyMissing = errors.New("secret is missing key")

const (
	serverReadTimeout     = 5 * time.Second
	serverWriteTimeout    = 10 * time.Second
	serverIdleTimeout     = 30 * time.Second
	serverShutdownTimeout = 10 * time.Second
	executionTimeout      = 5 * time.Second
)

func main() {
	var env envConfig
	if err := envconfig.Process("", &env); err != nil {
		slog.Default().Error("processing env var", "error", err)
		os.Exit(1)
	}

	var handler slog.Handler = slog.NewJSONHandler(
		os.Stdout,
		&slog.HandlerOptions{
			Level:       env.LogLevel,
			AddSource:   false,
			ReplaceAttr: nil,
		},
	)

	logger := slog.New(handler)

	if err := run(env, logger); err != nil {
		logger.Error("error running cain webhook", "error", err)
		os.Exit(1)
	}
}

func run(env envConfig, log *slog.Logger) error { //nolint: cyclop,funlen // hard to reduce ifs that are mainly for err checking
	log.Info("cain webhook starting",
		"version", version.Version,
		"revision", version.Revision,
		"build_date", version.BuildDate,
		"os", version.GoOS,
		"os_arch", version.GoArch,
		"go_version", version.GoVersion,
	)

	log = log.With("version", version.Version)

	metricsMux := http.NewServeMux()

	// initialise app metrics
	metrics, promRegistry, err := metrics.NewPrometheus(env.MetricsSubsystem)
	if err != nil {
		return fmt.Errorf("setting up prometheus metrics: %w", err)
	}

	log.Info("initialised metrics and prometheus registry")

	// initialise default K8s clientset client
	config, err := rest.InClusterConfig()
	if err != nil {
		return fmt.Errorf("getting K8s in-cluster config: %w", err)
	}

	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		return fmt.Errorf("creating K8s client: %w", err)
	}

	ctx, cancel := context.WithTimeout(
		context.Background(), executionTimeout,
	)
	defer cancel()

	// create the secret creator, responsible for creating secrets
	secretCreator, secretCreationChan, err := secrets.NewCreator(client, log.With("component", "secretcreator"), metrics)
	if err != nil {
		return fmt.Errorf("creating secret creator: %w", err)
	}

	// create the secret deletor, responsible for deleting secrets
	secretDeletor, secretDeletionChan, err := secrets.NewDeleter(client, log.With("component", "secretdeleter"), metrics)
	if err != nil {
		return fmt.Errorf("creating secret creator: %w", err)
	}

	// create the cert creator, responsible for creating cert-manager Certificates with a truststore
	// for use by the JVM
	certCreator, certCreatorChan, err := certificates.NewCreator(
		env.CAIssuer,
		secretCreationChan,
		log.With("component", "certcreator"),
		metrics,
	)
	if err != nil {
		return fmt.Errorf("creating certificate creator: %w", err)
	}

	// create the prometheus HTTP handler
	promHandler := promhttp.InstrumentMetricHandler(
		promRegistry,
		promhttp.HandlerFor(
			promRegistry,
			promhttp.HandlerOpts{},
		),
	)
	// add the prometheus handler to the HTTP server
	metricsMux.Handle("/metrics", promHandler)
	httpServer := http.Server{
		Addr:         ":" + env.MetricsPort,
		Handler:      metricsMux,
		ReadTimeout:  serverReadTimeout,
		WriteTimeout: serverWriteTimeout,
		IdleTimeout:  serverIdleTimeout,
	}

	log.Info("initialised HTTP metrics server")

	tlsCert, err := localhttp.NewReloadingTLSCert(env.TLSCertFile, env.TLSKeyFile, log)
	if err != nil {
		return fmt.Errorf("creating TLS certificate reloader: %w", err)
	}

	whServer, err := setupWebhooks(ctx, webhookDependencies{
		k8sClient:        client,
		secCreationChan:  secretCreationChan,
		secDeletionChan:  secretDeletionChan,
		certCreationChan: certCreatorChan,
		promRegistry:     promRegistry,
		tlsCert:          tlsCert,
	}, env, log)
	if err != nil {
		return fmt.Errorf("setting up webhooks: %w", err)
	}

	// add the signals SIGINT and SIGTERM for signaling the application to shutdown
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// create an context pool for shutting down the various goroutines if 1 of them returns an error
	ctxPool := pool.New().
		WithContext(ctx).
		WithFirstError().
		WithCancelOnError()

	// start the various goroutines within the context pool
	ctxPool.Go(func(ctx context.Context) error {
		if err := tlsCert.Start(ctx); err != nil {
			log.ErrorContext(ctx, "TLS cert reloader", "error", err)

			return fmt.Errorf("TLS cert reloader: %w", err)
		}

		return nil
	})

	ctxPool.Go(func(ctx context.Context) error {
		if err := secretCreator.Start(ctx); err != nil {
			log.ErrorContext(ctx, "secret creator", "error", err)

			return fmt.Errorf("secret creator: %w", err)
		}

		return nil
	})
	ctxPool.Go(func(ctx context.Context) error {
		if err := secretDeletor.Start(ctx); err != nil {
			log.ErrorContext(ctx, "secret deletor", "error", err)

			return fmt.Errorf("secret deletor: %w", err)
		}

		return nil
	})
	ctxPool.Go(func(ctx context.Context) error {
		if err := certCreator.Start(ctx); err != nil {
			log.ErrorContext(ctx, "cert creator", "error", err)

			return fmt.Errorf("cert creator: %w", err)
		}

		return nil
	})
	ctxPool.Go(func(_ context.Context) error {
		if err := httpServer.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
			log.ErrorContext(ctx, "http server", "error", err)

			return fmt.Errorf("metrics http server crashed: %w", err)
		}

		return nil
	})
	ctxPool.Go(func(ctx context.Context) error {
		// we wait for the context to be cancelled to signal the metrics server to shutdown,
		// this enables gracefully stopping any current requests
		<-ctx.Done()

		ctx, cancel := context.WithTimeout(context.Background(), serverShutdownTimeout)
		defer cancel()

		if err := httpServer.Shutdown(ctx); err != nil { //nolint:contextcheck // this is a bug https://github.com/kkHAIKE/contextcheck/issues/2
			log.ErrorContext(ctx, "http server shutdown", "error", err) //nolint:contextcheck // this is a bug https://github.com/kkHAIKE/contextcheck/issues/2

			return fmt.Errorf("shutting down metrics server: %w", err)
		}

		return nil
	})
	ctxPool.Go(func(_ context.Context) error {
		if err := whServer.ListenAndServeTLS("", ""); !errors.Is(err, http.ErrServerClosed) {
			log.ErrorContext(ctx, "webhook http server", "error", err)

			return fmt.Errorf("webhook http server crashed: %w", err)
		}

		return nil
	})
	ctxPool.Go(func(ctx context.Context) error {
		// we wait for the context to be cancelled to signal the webhook server to shutdown,
		// this enables gracefully stopping any current requests
		<-ctx.Done()

		ctx, cancel := context.WithTimeout(context.Background(), serverShutdownTimeout)
		defer cancel()

		if err := whServer.Shutdown(ctx); err != nil { //nolint:contextcheck // this is a bug https://github.com/kkHAIKE/contextcheck/issues/2
			log.ErrorContext(ctx, "webhook http server", "error", err) //nolint:contextcheck // this is a bug https://github.com/kkHAIKE/contextcheck/issues/2

			return fmt.Errorf("shutting down webhook server: %w", err)
		}

		return nil
	})

	// waits for all the goroutines to finish, basically keeping the main process running
	if err := ctxPool.Wait(); err != nil {
		return fmt.Errorf("error group had an error: %w", err)
	}

	return nil
}

type webhookDependencies struct {
	k8sClient        kubernetes.Interface
	secCreationChan  chan<- secrets.CreationRequest
	secDeletionChan  chan<- secrets.DeletionRequest
	certCreationChan chan<- certificates.Info
	promRegistry     prometheus.Registerer
	tlsCert          *localhttp.ReloadingTLSCert
}

func setupWebhooks( //nolint: cyclop,funlen // hard to reduce ifs that are mainly for err checking
	ctx context.Context,
	deps webhookDependencies,
	env envConfig,
	log *slog.Logger,
) (*http.Server, error) {
	executionNamespace, err := utils.GetPodNS()
	if err != nil {
		return nil, fmt.Errorf("getting Pod execution namespace: %w", err)
	}

	caSecret, err := deps.k8sClient.CoreV1().Secrets(executionNamespace).Get(ctx, env.CASecret.Name(), metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("getting default CA secret from K8s: %w", err)
	}

	caSecretData := make(map[string][]byte, len(env.CASecret.Keys()))

	for _, secretDataKey := range env.CASecret.Keys() {
		secretData, ok := caSecret.Data[secretDataKey]
		if !ok {
			return nil, fmt.Errorf("default CA secret value for key=%s: %w", secretDataKey, ErrSecretKeyMissing)
		}

		caSecretData[secretDataKey] = secretData
	}

	kwhLog := utils.NewLogger(log.With("component", "webhook"))
	extractor := metadata.NewExtractor(env.MetadataDomain, env.TruststorePassword)

	valWh, err := kwhvalidating.NewWebhook(kwhvalidating.WebhookConfig{
		ID: "cain-validation",
		Validator: webhook.NewValidator(
			extractor,
			deps.k8sClient,
			env.CASecret,
			caSecretData,
			deps.secCreationChan,
			deps.secDeletionChan,
			deps.certCreationChan,
			log.With("component", "validator"),
		),
		Logger: kwhLog,
		Obj:    &corev1.Pod{},
	})
	if err != nil {
		return nil, fmt.Errorf("creating validating webhook: %w", err)
	}

	containerResources, err := webhook.NewContainerResources(env.ContainerResourcesEnv)
	if err != nil {
		return nil, fmt.Errorf("parsing container resources: %w", err)
	}

	debianInitImage := fmt.Sprintf("%s:%s", env.DebianInitImage, version.Version)
	redhatInitImage := fmt.Sprintf("%s:%s", env.RedHatInitImage, version.Version)

	if env.DebianInitTag != "" {
		debianInitImage = fmt.Sprintf("%s:%s", env.DebianInitImage, env.DebianInitTag)
	}

	if env.RedHatInitTag != "" {
		redhatInitImage = fmt.Sprintf("%s:%s", env.RedHatInitImage, env.RedHatInitTag)
	}

	// create the K8s mutating webhook
	mutWh, err := kwhmutating.NewWebhook(kwhmutating.WebhookConfig{
		ID: "cain-mutation",
		Mutator: webhook.NewMutator(
			extractor,
			deps.k8sClient,
			env.CASecret,
			debianInitImage, redhatInitImage,
			env.JVMEnvVariable,
			containerResources,
			log.With("component", "mutator"),
		),
		Logger: kwhLog,
		Obj:    &corev1.Pod{},
	})
	if err != nil {
		return nil, fmt.Errorf("creating mutating webhook: %w", err)
	}

	// Add the prometheus registry to the webhook for recording webhook metrics
	kwhRecorder, err := kwhprometheus.NewRecorder(
		kwhprometheus.RecorderConfig{
			Registry:        deps.promRegistry,
			ReviewOpBuckets: nil,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("creating webhook metrics recorder: %w", err)
	}

	// create the HTTP handler for the validating webhook
	valHandler, err := kwhhttp.HandlerFor(kwhhttp.HandlerConfig{
		Webhook: kwhwebhook.NewMeasuredWebhook(kwhRecorder, valWh),
		Logger:  kwhLog,
		Tracer:  nil,
	})
	if err != nil {
		return nil, fmt.Errorf("creating validating webhook handler: %w", err)
	}

	// create the HTTP handler for the mutating webhook
	mutHandler, err := kwhhttp.HandlerFor(kwhhttp.HandlerConfig{
		Webhook: kwhwebhook.NewMeasuredWebhook(kwhRecorder, mutWh),
		Logger:  kwhLog,
		Tracer:  nil,
	})
	if err != nil {
		return nil, fmt.Errorf("creating mutating webhook handler: %w", err)
	}

	// create the HTTP server mux for the webhook
	whMux := http.NewServeMux()
	// add the validating webhook handler at the path "/inject/validate"
	whMux.Handle("/inject/validate", valHandler)
	// add the mutating webhook handler at the path "/inject/mutate"
	whMux.Handle("/inject/mutate", mutHandler)

	whServer := http.Server{
		Addr:    ":" + env.Port,
		Handler: whMux,
		TLSConfig: &tls.Config{
			GetCertificate: deps.tlsCert.GetCertificate,
			MinVersion:     tls.VersionTLS12,
		},
		ReadTimeout:  serverReadTimeout,
		WriteTimeout: serverWriteTimeout,
		IdleTimeout:  serverIdleTimeout,
	}

	log.Info("initialised HTTPS Webhook server")

	return &whServer, nil
}
