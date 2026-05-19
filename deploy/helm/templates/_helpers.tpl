{{- /*
Lantern Helm Chart — Shared Helpers
*/ -}}

{{- /*
lantern.name returns a sanitized release name.
*/ -}}
{{- define "lantern.name" -}}
{{- default .Release.Name .Values.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- /*
lantern.labels returns standard Kubernetes labels shared across all resources.
*/ -}}
{{- define "lantern.labels" -}}
app.kubernetes.io/name: lantern
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{- /*
lantern.selectorLabels returns labels used in selector matchLabels.
*/ -}}
{{- define "lantern.selectorLabels" -}}
app.kubernetes.io/name: lantern
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{- /*
lantern.componentLabels returns labels for a specific component.

Usage: {{ include "lantern.componentLabels" (merge (dict "component" "writer") .) }}
*/ -}}
{{- define "lantern.componentLabels" -}}
app.kubernetes.io/component: {{ .component }}
{{- end -}}

{{- /*
lantern.fullLabels renders full labels with proper indentation.

Usage: {{ include "lantern.fullLabels" (merge (dict "component" "writer" "indent" 4) .) | nindent .indent }}
*/ -}}
{{- define "lantern.fullLabels" -}}
{{- $labels := include "lantern.labels" . -}}
{{- $compLabels := include "lantern.componentLabels" (merge (dict "component" .component) .) -}}
{{ $labels | trim }}
{{ $compLabels | trim }}
{{- end -}}

{{- /*
lantern.redisPassword determines whether a Redis password reference should be included.

Returns "redis-password" if Redis password should be configured (via internal auth
or external connection), empty string otherwise.

Usage: {{ include "lantern.redisPassword" . }}
*/ -}}
{{- define "lantern.redisPassword" -}}
{{- if and .Values.redis.enabled .Values.redis.auth.enabled .Values.redis.auth.password -}}
redis-password
{{- else if .Values.redis.external.password -}}
redis-password
{{- end }}
{{- end -}}

{{- /*
lantern.storageEndpoint returns the S3-compatible storage endpoint URL.

Returns empty string if no storage is configured.

Usage: {{ include "lantern.storageEndpoint" . }}
*/ -}}
{{- define "lantern.storageEndpoint" -}}
{{- if .Values.minio.enabled -}}
{{- printf "http://%s-minio:9000" .Release.Name -}}
{{- else if .Values.storage.endpoint -}}
{{- .Values.storage.endpoint -}}
{{- else if .Values.minio.external.endpoint -}}
{{- .Values.minio.external.endpoint -}}
{{- end }}
{{- end -}}

{{- /*
lantern.storageAccessKey returns the S3 access key.

Usage: {{ include "lantern.storageAccessKey" . }}
*/ -}}
{{- define "lantern.storageAccessKey" -}}
{{- if .Values.storage.accessKey -}}
{{- .Values.storage.accessKey -}}
{{- else if .Values.minio.enabled -}}
{{- .Values.minio.rootUser -}}
{{- else if .Values.minio.external.accessKey -}}
{{- .Values.minio.external.accessKey -}}
{{- end }}
{{- end -}}

{{- /*
lantern.storageSecretKey returns the S3 secret key.

Usage: {{ include "lantern.storageSecretKey" . }}
*/ -}}
{{- define "lantern.storageSecretKey" -}}
{{- if .Values.storage.secretKey -}}
{{- .Values.storage.secretKey -}}
{{- else if .Values.minio.enabled -}}
{{- .Values.minio.rootPassword -}}
{{- else if .Values.minio.external.secretKey -}}
{{- .Values.minio.external.secretKey -}}
{{- end }}
{{- end -}}

{{- /*
lantern.postgresDsn returns the PostgreSQL DSN string.

Usage: {{ include "lantern.postgresDsn" . }}
*/ -}}
{{- define "lantern.postgresDsn" -}}
{{- if .Values.postgresql.enabled -}}
{{- printf "postgres://%s:%s@%s-postgresql:5432/%s?sslmode=disable"
  .Values.postgresql.auth.username
  .Values.postgresql.auth.password
  .Release.Name
  .Values.postgresql.auth.database -}}
{{- else if .Values.postgresql.external.host -}}
{{- printf "postgres://%s:%s@%s:%d/%s?sslmode=%s"
  .Values.postgresql.external.user
  .Values.postgresql.external.password
  .Values.postgresql.external.host
  .Values.postgresql.external.port
  .Values.postgresql.external.database
  .Values.postgresql.external.sslmode -}}
{{- else if .Values.database.dsn -}}
{{- .Values.database.dsn -}}
{{- end }}
{{- end -}}

{{- /*
lantern.redisAddr returns the Redis connection address.

Usage: {{ include "lantern.redisAddr" . }}
*/ -}}
{{- define "lantern.redisAddr" -}}
{{- if .Values.redis.enabled -}}
{{- printf "%s-redis:6379" .Release.Name -}}
{{- else if .Values.redis.external.addr -}}
{{- .Values.redis.external.addr -}}
{{- end }}
{{- end -}}
