# CAIn 

Admission webhook for injecting CA certificates into Pods.

## TL;DR;

```bash
helm install <NAME> ./ --create-namespace -n <NAMESPACE> -f values.yaml
```

## Values

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| imagePullSecrets| list of strings | [] | Name of pull secrets to use. |
| replicaCount | int | `1` | Number of replicas (pods) to launch. |
| image.repository | string | `"ghcr.io/weisshorn-cyd/cain"` | Name of the image. |
| image.pullPolicy | string | `"IfNotPresent"` | [Image pull policy](https://kubernetes.io/docs/concepts/containers/images/#updating-images) for updating already existing images on a node. |
| image.tag | string | `""` | Image tag override for the default value (chart appVersion). |
| redhatInitImage.repository | string | `"ghcr.io/weisshorn-cyd/cain-redhat-init"` | Name of the image repository to pull the Red Hat container image from. |
| redhatInitImage.tag | string | `""` | Red Hat image tag override for the default value (chart appVersion). |
| debianInitImage.repository | string | `"ghcr.io/weisshorn-cyd/cain-debian-init"` | Name of the image repository to pull the Debian container image from. |
| debianInitImage.tag | string | `""` | Debian image tag override for the default value (chart appVersion). |
| nameOverride | string | `""` | A name in place of the chart name for `app:` labels. |
| fullnameOverride | string | `""` | A name to substitute for the full names of resources. |
| resources.limits.cpu | string | `"1000m"` | CPU limit for the cain webhook container. |
| resources.limits.memory | string | `"512Mi"` | Memory limit for the cain webhook container. |
| resources.requests.cpu | string | `"100m"` | CPU request for the cain webhook container. |
| resources.limits.cpu | string | `"512Mi"` | Memory request for the cain webhook container. |
| caInjectionInitcontainer.resources.limits.cpu  | string  | `"500m"`  | CPU limit for the CA injection init container.  |
| caInjectionInitcontainer.resources.limits.memory | string  | `"50Mi"`  | Memory limit for the CA injection init container.  |
| caInjectionInitcontainer.resources.requests.cpu | string  | `""`  | CPU requests for the CA injection initcontainer, defaults to limits.cpu.  |
| caInjectionInitcontainer.resources.requests.memory | string   | `""`   | Memory requests for the CA injection initcontainer, defaults to limits.memory. |
| config.metadataDomain | string | `"weisshorn.cyd"` | The domain name for the enabling label. |
| config.jvmEnvVar | string | `"JAVA_OPTS_CUSTOM"` | The environment variable that should be set to configure the JVM where to read the truststore. |
| config.injectorIssuer | string | `"cert-issuer"` | The name of the Cert-Manager issuer to use for generating certificates containing a truststore. |
| config.truststorePassword | string | `"injected-ca"` | The injected truststore password. |
| config.logLevel | string | `"info"` | The webhook log level. |
| containerPort | int | `8443` | Webhook container port. |
| metricsPort | int | `8080` | Webhook metrics port. |
| caSecret.name | string | `inject-ca` | The secret that contains a CA certificate that should be injected. |
| caSecret.key | string | `ca.crt` | The secret key that holds the CA certificate that should be injected. |
| podAnnotations | object | `{}` | Annotations to add to the pod. | 
| serviceAccount.annotations | object | `{}` | Annotations to be added to the service account. |
| serviceAccount.name | string | `""` | The name of the service account to use. If not set and create is true, a name is generated using the fullname template. |
| dummyCertificate.domainName | string | `"cain.weisshorn.cyd"` | The domain name for the dummy certificate. |
| dummyCertificate.issuer.kind | string | `"ClusterIssuer"` | The kind of Cert Manager issuer to use to create the dummy certificate. |
| dummyCertificate.issuer.name | string | `"cert-issuer"` | The name of Cert Manager issuer to use to create the dummy certificate. |
