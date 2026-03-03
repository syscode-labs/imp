# Helm Chart Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Package Imp as two Helm charts (`imp-crds` and `imp`) ready for production GitOps deployment.

**Architecture:** `imp-crds` holds the six CRDs (with `helm.sh/resource-policy: keep`) for independent lifecycle management. `imp` holds operator Deployment, agent DaemonSet, RBAC, and webhook resources (cert-manager Certificate + Issuer + webhook configurations). All templates use `.Release.Namespace` for full namespace-awareness.

**Tech Stack:** Helm v3, helm-unittest (plugin), cert-manager v1 API, controller-runtime webhook paths.

**Worktree:** `~/.config/superpowers/worktrees/imp/helm-chart`
All commands run from that directory unless noted.

**Run tests with:**
```bash
KUBEBUILDER_ASSETS="$(/Users/giovanni/syscode/git/imp/bin/setup-envtest use --bin-dir /Users/giovanni/syscode/git/imp/bin/k8s -p path 2>/dev/null)" go test ./...
helm unittest charts/imp-crds charts/imp
```

---

### Task 1: Install helm-unittest + imp-crds chart

**Files:**
- Create: `charts/imp-crds/Chart.yaml`
- Create: `charts/imp-crds/templates/impvms.yaml`
- Create: `charts/imp-crds/templates/impvmclasses.yaml`
- Create: `charts/imp-crds/templates/impvmtemplates.yaml`
- Create: `charts/imp-crds/templates/impnetworks.yaml`
- Create: `charts/imp-crds/templates/clusterimpconfigs.yaml`
- Create: `charts/imp-crds/templates/clusterimpnodeprofiles.yaml`

**Step 1: Install helm-unittest plugin**

```bash
helm plugin install https://github.com/helm-unittest/helm-unittest.git
```

Expected: `Installed plugin: unittest`

**Step 2: Create `charts/imp-crds/Chart.yaml`**

```yaml
apiVersion: v2
name: imp-crds
description: CRDs for the Imp Firecracker microVM operator
type: application
version: 0.1.0
appVersion: "0.1.0"
keywords:
  - firecracker
  - microvm
  - kubernetes
home: https://github.com/syscode-labs/imp
```

**Step 3: Create each CRD template**

For each CRD, copy the source file from `config/crd/bases/` and add ONE annotation to the `metadata.annotations` block. The annotation prevents Helm from deleting CRDs on `helm uninstall`.

For each file listed below:
- Source: `config/crd/bases/<source>`
- Dest: `charts/imp-crds/templates/<dest>`
- Add under `metadata:` → `annotations:` → `"helm.sh/resource-policy": keep`

| Source | Dest |
|--------|------|
| `imp.dev_impvms.yaml` | `impvms.yaml` |
| `imp.dev_impvmclasses.yaml` | `impvmclasses.yaml` |
| `imp.dev_impvmtemplates.yaml` | `impvmtemplates.yaml` |
| `imp.dev_impnetworks.yaml` | `impnetworks.yaml` |
| `imp.dev_clusterimpconfigs.yaml` | `clusterimpconfigs.yaml` |
| `imp.dev_clusterimpnodeprofiles.yaml` | `clusterimpnodeprofiles.yaml` |

Example — the `metadata:` section of each file should look like:
```yaml
metadata:
  annotations:
    "helm.sh/resource-policy": keep
    controller-gen.kubebuilder.io/version: ...  # already present, keep it
  name: impvms.imp.dev   # already present, keep it
```

**Step 4: Lint**

```bash
helm lint charts/imp-crds
```

Expected: `1 chart(s) linted, 0 chart(s) failed`

**Step 5: Commit**

```bash
git add charts/imp-crds
git commit -m "feat(helm): imp-crds chart — six CRDs with resource-policy keep"
```

---

### Task 2: imp chart scaffold — Chart.yaml + values.yaml + _helpers.tpl

**Files:**
- Create: `charts/imp/Chart.yaml`
- Create: `charts/imp/values.yaml`
- Create: `charts/imp/templates/_helpers.tpl`

**Step 1: Write the failing lint test**

```bash
helm lint charts/imp 2>&1 | head -5
```

Expected: `Error: stat charts/imp: no such file or directory`

**Step 2: Create `charts/imp/Chart.yaml`**

```yaml
apiVersion: v2
name: imp
description: Imp — Firecracker microVM operator for Kubernetes
type: application
version: 0.1.0
appVersion: "0.1.0"
keywords:
  - firecracker
  - microvm
  - kubernetes
home: https://github.com/syscode-labs/imp
```

**Step 3: Create `charts/imp/values.yaml`**

```yaml
nameOverride: ""
fullnameOverride: ""
imagePullSecrets: []

operator:
  image:
    repository: ghcr.io/syscode-labs/imp-operator
    tag: ""
    pullPolicy: IfNotPresent
  replicaCount: 1
  resources:
    requests:
      cpu: 100m
      memory: 64Mi
    limits:
      memory: 128Mi
  nodeSelector: {}
  rbac:
    cniDetection: minimal

agent:
  image:
    repository: ghcr.io/syscode-labs/imp-agent
    tag: ""
    pullPolicy: IfNotPresent
  resources:
    requests:
      cpu: 100m
      memory: 64Mi
    limits:
      memory: 128Mi
  nodeSelector: {}
  env:
    kernelPath: ""
  hostPaths:
    socketDir:
      enabled: false
      path: /run/imp/sockets
    imageCache:
      enabled: false
      path: /var/lib/imp/images

webhook:
  certManager:
    enabled: true
    issuerRef: {}
```

**Step 4: Create `charts/imp/templates/_helpers.tpl`**

```
{{/*
Expand the name of the chart.
*/}}
{{- define "imp.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
*/}}
{{- define "imp.fullname" -}}
{{- if .Values.fullnameOverride }}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- $name := default .Chart.Name .Values.nameOverride }}
{{- if contains $name .Release.Name }}
{{- .Release.Name | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- end }}
{{- end }}

{{/*
Chart label.
*/}}
{{- define "imp.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels.
*/}}
{{- define "imp.labels" -}}
helm.sh/chart: {{ include "imp.chart" . }}
{{ include "imp.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels.
*/}}
{{- define "imp.selectorLabels" -}}
app.kubernetes.io/name: {{ include "imp.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Operator ServiceAccount name.
*/}}
{{- define "imp.operator.serviceAccountName" -}}
{{- include "imp.fullname" . }}-operator
{{- end }}

{{/*
Agent ServiceAccount name.
*/}}
{{- define "imp.agent.serviceAccountName" -}}
{{- include "imp.fullname" . }}-agent
{{- end }}

{{/*
Image tag — falls back to .Chart.AppVersion when tag is empty.
*/}}
{{- define "imp.operator.image" -}}
{{- $tag := .Values.operator.image.tag | default .Chart.AppVersion }}
{{- printf "%s:%s" .Values.operator.image.repository $tag }}
{{- end }}

{{- define "imp.agent.image" -}}
{{- $tag := .Values.agent.image.tag | default .Chart.AppVersion }}
{{- printf "%s:%s" .Values.agent.image.repository $tag }}
{{- end }}
```

**Step 5: Lint**

```bash
helm lint charts/imp
```

Expected: `1 chart(s) linted, 0 chart(s) failed`

**Step 6: Commit**

```bash
git add charts/imp
git commit -m "feat(helm): imp chart scaffold — Chart.yaml, values.yaml, _helpers.tpl"
```

---

### Task 3: Operator RBAC

**Files:**
- Create: `charts/imp/templates/operator/serviceaccount.yaml`
- Create: `charts/imp/templates/operator/clusterrole.yaml`
- Create: `charts/imp/templates/operator/clusterrole-full.yaml`
- Create: `charts/imp/templates/operator/clusterrolebinding.yaml`
- Create: `charts/imp/templates/operator/role.yaml`
- Create: `charts/imp/templates/operator/rolebinding.yaml`
- Create: `charts/imp/tests/operator-rbac_test.yaml`

**Step 1: Write the failing test** (`charts/imp/tests/operator-rbac_test.yaml`)

```yaml
suite: operator RBAC
templates:
  - templates/operator/serviceaccount.yaml
  - templates/operator/clusterrole.yaml
  - templates/operator/clusterrole-full.yaml
  - templates/operator/clusterrolebinding.yaml
  - templates/operator/role.yaml
  - templates/operator/rolebinding.yaml
tests:
  - it: creates operator ServiceAccount
    template: templates/operator/serviceaccount.yaml
    asserts:
      - isKind:
          of: ServiceAccount
      - equal:
          path: metadata.name
          value: RELEASE-NAME-imp-operator

  - it: creates minimal ClusterRole
    template: templates/operator/clusterrole.yaml
    asserts:
      - isKind:
          of: ClusterRole
      - contains:
          path: rules
          content:
            apiGroups: ["imp.dev"]
            resources:
              - impvms
              - impvmclasses
              - impvmtemplates
              - impnetworks
              - clusterimpconfigs
              - clusterimpnodeprofiles
            verbs: [get, list, watch, update, patch]

  - it: does not create full ClusterRole by default
    template: templates/operator/clusterrole-full.yaml
    set:
      operator.rbac.cniDetection: minimal
    asserts:
      - hasDocuments:
          count: 0

  - it: creates full ClusterRole when cniDetection is full
    template: templates/operator/clusterrole-full.yaml
    set:
      operator.rbac.cniDetection: full
    asserts:
      - hasDocuments:
          count: 1
      - isKind:
          of: ClusterRole
      - contains:
          path: rules
          content:
            apiGroups: ["apps"]
            resources: [daemonsets]
            verbs: [get, list]

  - it: creates namespace-scoped Role for leader election
    template: templates/operator/role.yaml
    asserts:
      - isKind:
          of: Role
      - equal:
          path: metadata.namespace
          value: NAMESPACE
```

**Step 2: Run test, verify it fails**

```bash
helm unittest charts/imp 2>&1 | tail -10
```

Expected: FAIL — templates not found

**Step 3: Create `charts/imp/templates/operator/serviceaccount.yaml`**

```yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  name: {{ include "imp.operator.serviceAccountName" . }}
  namespace: {{ .Release.Namespace }}
  labels:
    {{- include "imp.labels" . | nindent 4 }}
```

**Step 4: Create `charts/imp/templates/operator/clusterrole.yaml`**

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: {{ include "imp.fullname" . }}-operator
  labels:
    {{- include "imp.labels" . | nindent 4 }}
rules:
- apiGroups: ["imp.dev"]
  resources:
    - impvms
    - impvmclasses
    - impvmtemplates
    - impnetworks
    - clusterimpconfigs
    - clusterimpnodeprofiles
  verbs: [get, list, watch, update, patch]
- apiGroups: ["imp.dev"]
  resources:
    - impvms/status
    - impnetworks/status
  verbs: [get, update, patch]
- apiGroups: ["imp.dev"]
  resources:
    - impvms/finalizers
    - impnetworks/finalizers
  verbs: [update]
- apiGroups: [""]
  resources: [nodes]
  verbs: [get, list, watch]
- apiGroups: [""]
  resources: [events]
  verbs: [create, patch]
- apiGroups: ["apiextensions.k8s.io"]
  resources: [customresourcedefinitions]
  verbs: [get, list]
```

**Step 5: Create `charts/imp/templates/operator/clusterrole-full.yaml`**

```yaml
{{- if eq .Values.operator.rbac.cniDetection "full" }}
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: {{ include "imp.fullname" . }}-operator-cni-full
  labels:
    {{- include "imp.labels" . | nindent 4 }}
rules:
- apiGroups: ["apps"]
  resources: [daemonsets]
  verbs: [get, list]
{{- end }}
```

**Step 6: Create `charts/imp/templates/operator/clusterrolebinding.yaml`**

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: {{ include "imp.fullname" . }}-operator
  labels:
    {{- include "imp.labels" . | nindent 4 }}
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: {{ include "imp.fullname" . }}-operator
subjects:
- kind: ServiceAccount
  name: {{ include "imp.operator.serviceAccountName" . }}
  namespace: {{ .Release.Namespace }}
{{- if eq .Values.operator.rbac.cniDetection "full" }}
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: {{ include "imp.fullname" . }}-operator-cni-full
  labels:
    {{- include "imp.labels" . | nindent 4 }}
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: {{ include "imp.fullname" . }}-operator-cni-full
subjects:
- kind: ServiceAccount
  name: {{ include "imp.operator.serviceAccountName" . }}
  namespace: {{ .Release.Namespace }}
{{- end }}
```

**Step 7: Create `charts/imp/templates/operator/role.yaml`**

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: {{ include "imp.fullname" . }}-operator-leader-election
  namespace: {{ .Release.Namespace }}
  labels:
    {{- include "imp.labels" . | nindent 4 }}
rules:
- apiGroups: [""]
  resources: [configmaps]
  verbs: [get, list, watch, create, update, patch, delete]
- apiGroups: ["coordination.k8s.io"]
  resources: [leases]
  verbs: [get, list, watch, create, update, patch, delete]
- apiGroups: [""]
  resources: [events]
  verbs: [create, patch]
```

**Step 8: Create `charts/imp/templates/operator/rolebinding.yaml`**

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: {{ include "imp.fullname" . }}-operator-leader-election
  namespace: {{ .Release.Namespace }}
  labels:
    {{- include "imp.labels" . | nindent 4 }}
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: Role
  name: {{ include "imp.fullname" . }}-operator-leader-election
subjects:
- kind: ServiceAccount
  name: {{ include "imp.operator.serviceAccountName" . }}
  namespace: {{ .Release.Namespace }}
```

**Step 9: Run tests**

```bash
helm unittest charts/imp
```

Expected: all operator RBAC tests pass

**Step 10: Commit**

```bash
git add charts/imp/templates/operator/ charts/imp/tests/
git commit -m "feat(helm): operator RBAC — ServiceAccount, ClusterRole, leader-election Role"
```

---

### Task 4: Operator Deployment + Service

**Files:**
- Create: `charts/imp/templates/operator/deployment.yaml`
- Create: `charts/imp/templates/operator/service.yaml`
- Modify: `charts/imp/tests/operator-rbac_test.yaml` → add new test file instead
- Create: `charts/imp/tests/operator-deployment_test.yaml`

**Step 1: Write the failing test** (`charts/imp/tests/operator-deployment_test.yaml`)

```yaml
suite: operator Deployment and Service
templates:
  - templates/operator/deployment.yaml
  - templates/operator/service.yaml
tests:
  - it: renders Deployment with correct image
    template: templates/operator/deployment.yaml
    set:
      agent.env.kernelPath: /usr/local/bin/firecracker
    asserts:
      - isKind:
          of: Deployment
      - equal:
          path: spec.template.spec.containers[0].image
          value: ghcr.io/syscode-labs/imp-operator:0.1.0
      - equal:
          path: spec.replicas
          value: 1

  - it: uses custom image tag when set
    template: templates/operator/deployment.yaml
    set:
      agent.env.kernelPath: /usr/local/bin/firecracker
      operator.image.tag: v1.2.3
    asserts:
      - equal:
          path: spec.template.spec.containers[0].image
          value: ghcr.io/syscode-labs/imp-operator:v1.2.3

  - it: mounts webhook TLS secret
    template: templates/operator/deployment.yaml
    set:
      agent.env.kernelPath: /usr/local/bin/firecracker
    asserts:
      - contains:
          path: spec.template.spec.volumes
          content:
            name: webhook-tls
            secret:
              secretName: imp-webhook-tls

  - it: renders Service with webhook and metrics ports
    template: templates/operator/service.yaml
    asserts:
      - isKind:
          of: Service
      - contains:
          path: spec.ports
          content:
            name: webhook
            port: 9443
            targetPort: 9443
      - contains:
          path: spec.ports
          content:
            name: metrics
            port: 8080
            targetPort: 8080
```

**Step 2: Run test, verify it fails**

```bash
helm unittest charts/imp 2>&1 | tail -10
```

Expected: FAIL — templates not found

**Step 3: Create `charts/imp/templates/operator/deployment.yaml`**

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: {{ include "imp.fullname" . }}-operator
  namespace: {{ .Release.Namespace }}
  labels:
    {{- include "imp.labels" . | nindent 4 }}
    app.kubernetes.io/component: operator
spec:
  replicas: {{ .Values.operator.replicaCount }}
  selector:
    matchLabels:
      {{- include "imp.selectorLabels" . | nindent 6 }}
      app.kubernetes.io/component: operator
  template:
    metadata:
      labels:
        {{- include "imp.selectorLabels" . | nindent 8 }}
        app.kubernetes.io/component: operator
    spec:
      {{- with .Values.imagePullSecrets }}
      imagePullSecrets:
        {{- toYaml . | nindent 8 }}
      {{- end }}
      serviceAccountName: {{ include "imp.operator.serviceAccountName" . }}
      securityContext:
        runAsNonRoot: true
        seccompProfile:
          type: RuntimeDefault
      {{- with .Values.operator.nodeSelector }}
      nodeSelector:
        {{- toYaml . | nindent 8 }}
      {{- end }}
      containers:
      - name: operator
        image: {{ include "imp.operator.image" . }}
        imagePullPolicy: {{ .Values.operator.image.pullPolicy }}
        args:
          - --leader-elect
          - --health-probe-bind-address=:8081
        ports:
        - name: webhook
          containerPort: 9443
          protocol: TCP
        - name: metrics
          containerPort: 8080
          protocol: TCP
        - name: healthz
          containerPort: 8081
          protocol: TCP
        securityContext:
          allowPrivilegeEscalation: false
          readOnlyRootFilesystem: true
          capabilities:
            drop: ["ALL"]
        livenessProbe:
          httpGet:
            path: /healthz
            port: 8081
          initialDelaySeconds: 15
          periodSeconds: 20
        readinessProbe:
          httpGet:
            path: /readyz
            port: 8081
          initialDelaySeconds: 5
          periodSeconds: 10
        resources:
          {{- toYaml .Values.operator.resources | nindent 10 }}
        volumeMounts:
        - name: webhook-tls
          mountPath: /tmp/k8s-webhook-server/serving-certs
          readOnly: true
      volumes:
      - name: webhook-tls
        secret:
          secretName: imp-webhook-tls
      terminationGracePeriodSeconds: 10
```

**Step 4: Create `charts/imp/templates/operator/service.yaml`**

```yaml
apiVersion: v1
kind: Service
metadata:
  name: {{ include "imp.fullname" . }}-operator
  namespace: {{ .Release.Namespace }}
  labels:
    {{- include "imp.labels" . | nindent 4 }}
    app.kubernetes.io/component: operator
spec:
  selector:
    {{- include "imp.selectorLabels" . | nindent 4 }}
    app.kubernetes.io/component: operator
  ports:
  - name: webhook
    port: 9443
    targetPort: 9443
    protocol: TCP
  - name: metrics
    port: 8080
    targetPort: 8080
    protocol: TCP
```

**Step 5: Run tests**

```bash
helm unittest charts/imp
```

Expected: all tests pass

**Step 6: Commit**

```bash
git add charts/imp/templates/operator/deployment.yaml charts/imp/templates/operator/service.yaml charts/imp/tests/operator-deployment_test.yaml
git commit -m "feat(helm): operator Deployment and Service"
```

---

### Task 5: Agent RBAC

**Files:**
- Create: `charts/imp/templates/agent/serviceaccount.yaml`
- Create: `charts/imp/templates/agent/clusterrole.yaml`
- Create: `charts/imp/templates/agent/clusterrolebinding.yaml`
- Create: `charts/imp/tests/agent-rbac_test.yaml`

**Step 1: Write the failing test** (`charts/imp/tests/agent-rbac_test.yaml`)

```yaml
suite: agent RBAC
templates:
  - templates/agent/serviceaccount.yaml
  - templates/agent/clusterrole.yaml
  - templates/agent/clusterrolebinding.yaml
tests:
  - it: creates agent ServiceAccount
    template: templates/agent/serviceaccount.yaml
    asserts:
      - isKind:
          of: ServiceAccount
      - equal:
          path: metadata.name
          value: RELEASE-NAME-imp-agent

  - it: creates agent ClusterRole with required rules
    template: templates/agent/clusterrole.yaml
    asserts:
      - isKind:
          of: ClusterRole
      - contains:
          path: rules
          content:
            apiGroups: ["imp.dev"]
            resources: [impvms]
            verbs: [get, list, watch, update, patch]
      - contains:
          path: rules
          content:
            apiGroups: ["imp.dev"]
            resources: [impvms/status]
            verbs: [get, update, patch]
      - contains:
          path: rules
          content:
            apiGroups: ["imp.dev"]
            resources: [impvmclasses]
            verbs: [get]
      - contains:
          path: rules
          content:
            apiGroups: ["imp.dev"]
            resources: [impnetworks]
            verbs: [get]

  - it: binds agent ClusterRole to agent ServiceAccount
    template: templates/agent/clusterrolebinding.yaml
    asserts:
      - equal:
          path: roleRef.name
          value: RELEASE-NAME-imp-agent
      - equal:
          path: subjects[0].name
          value: RELEASE-NAME-imp-agent
      - equal:
          path: subjects[0].namespace
          value: NAMESPACE
```

**Step 2: Run test, verify it fails**

```bash
helm unittest charts/imp 2>&1 | tail -5
```

Expected: FAIL

**Step 3: Create `charts/imp/templates/agent/serviceaccount.yaml`**

```yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  name: {{ include "imp.agent.serviceAccountName" . }}
  namespace: {{ .Release.Namespace }}
  labels:
    {{- include "imp.labels" . | nindent 4 }}
```

**Step 4: Create `charts/imp/templates/agent/clusterrole.yaml`**

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: {{ include "imp.fullname" . }}-agent
  labels:
    {{- include "imp.labels" . | nindent 4 }}
rules:
- apiGroups: ["imp.dev"]
  resources: [impvms]
  verbs: [get, list, watch, update, patch]
- apiGroups: ["imp.dev"]
  resources: [impvms/status]
  verbs: [get, update, patch]
- apiGroups: ["imp.dev"]
  resources: [impvmclasses]
  verbs: [get]
- apiGroups: ["imp.dev"]
  resources: [impnetworks]
  verbs: [get]
- apiGroups: [""]
  resources: [events]
  verbs: [create, patch]
```

**Step 5: Create `charts/imp/templates/agent/clusterrolebinding.yaml`**

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: {{ include "imp.fullname" . }}-agent
  labels:
    {{- include "imp.labels" . | nindent 4 }}
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: {{ include "imp.fullname" . }}-agent
subjects:
- kind: ServiceAccount
  name: {{ include "imp.agent.serviceAccountName" . }}
  namespace: {{ .Release.Namespace }}
```

**Step 6: Run tests**

```bash
helm unittest charts/imp
```

Expected: all tests pass

**Step 7: Commit**

```bash
git add charts/imp/templates/agent/ charts/imp/tests/agent-rbac_test.yaml
git commit -m "feat(helm): agent RBAC — ServiceAccount, ClusterRole, ClusterRoleBinding"
```

---

### Task 6: Agent DaemonSet

**Files:**
- Create: `charts/imp/templates/agent/daemonset.yaml`
- Create: `charts/imp/tests/agent-daemonset_test.yaml`

**Step 1: Write the failing test** (`charts/imp/tests/agent-daemonset_test.yaml`)

```yaml
suite: agent DaemonSet
templates:
  - templates/agent/daemonset.yaml
tests:
  - it: renders DaemonSet with correct image
    template: templates/agent/daemonset.yaml
    set:
      agent.env.kernelPath: /usr/local/bin/firecracker
    asserts:
      - isKind:
          of: DaemonSet
      - equal:
          path: spec.template.spec.containers[0].image
          value: ghcr.io/syscode-labs/imp-agent:0.1.0

  - it: fails when kernelPath is not set
    template: templates/agent/daemonset.yaml
    asserts:
      - failedTemplate:
          errorMessage: "agent.env.kernelPath is required"

  - it: always mounts /dev/kvm
    template: templates/agent/daemonset.yaml
    set:
      agent.env.kernelPath: /usr/local/bin/firecracker
    asserts:
      - contains:
          path: spec.template.spec.volumes
          content:
            name: dev-kvm
            hostPath:
              path: /dev/kvm

  - it: uses emptyDir for socketDir when hostPaths disabled
    template: templates/agent/daemonset.yaml
    set:
      agent.env.kernelPath: /usr/local/bin/firecracker
      agent.hostPaths.socketDir.enabled: false
    asserts:
      - contains:
          path: spec.template.spec.volumes
          content:
            name: socket-dir
            emptyDir: {}

  - it: uses hostPath for socketDir when enabled
    template: templates/agent/daemonset.yaml
    set:
      agent.env.kernelPath: /usr/local/bin/firecracker
      agent.hostPaths.socketDir.enabled: true
      agent.hostPaths.socketDir.path: /run/imp/sockets
    asserts:
      - contains:
          path: spec.template.spec.volumes
          content:
            name: socket-dir
            hostPath:
              path: /run/imp/sockets

  - it: uses emptyDir for imageCache when hostPaths disabled
    template: templates/agent/daemonset.yaml
    set:
      agent.env.kernelPath: /usr/local/bin/firecracker
      agent.hostPaths.imageCache.enabled: false
    asserts:
      - contains:
          path: spec.template.spec.volumes
          content:
            name: image-cache
            emptyDir: {}

  - it: uses hostPath for imageCache when enabled
    template: templates/agent/daemonset.yaml
    set:
      agent.env.kernelPath: /usr/local/bin/firecracker
      agent.hostPaths.imageCache.enabled: true
      agent.hostPaths.imageCache.path: /var/lib/imp/images
    asserts:
      - contains:
          path: spec.template.spec.volumes
          content:
            name: image-cache
            hostPath:
              path: /var/lib/imp/images
```

**Step 2: Run test, verify it fails**

```bash
helm unittest charts/imp 2>&1 | tail -5
```

Expected: FAIL — template not found

**Step 3: Create `charts/imp/templates/agent/daemonset.yaml`**

```yaml
apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: {{ include "imp.fullname" . }}-agent
  namespace: {{ .Release.Namespace }}
  labels:
    {{- include "imp.labels" . | nindent 4 }}
    app.kubernetes.io/component: agent
spec:
  selector:
    matchLabels:
      {{- include "imp.selectorLabels" . | nindent 6 }}
      app.kubernetes.io/component: agent
  template:
    metadata:
      labels:
        {{- include "imp.selectorLabels" . | nindent 8 }}
        app.kubernetes.io/component: agent
    spec:
      {{- with .Values.imagePullSecrets }}
      imagePullSecrets:
        {{- toYaml . | nindent 8 }}
      {{- end }}
      serviceAccountName: {{ include "imp.agent.serviceAccountName" . }}
      {{- with .Values.agent.nodeSelector }}
      nodeSelector:
        {{- toYaml . | nindent 8 }}
      {{- end }}
      containers:
      - name: agent
        image: {{ include "imp.agent.image" . }}
        imagePullPolicy: {{ .Values.agent.image.pullPolicy }}
        env:
        - name: NODE_NAME
          valueFrom:
            fieldRef:
              fieldPath: spec.nodeName
        - name: FC_KERNEL
          value: {{ required "agent.env.kernelPath is required" .Values.agent.env.kernelPath | quote }}
        resources:
          {{- toYaml .Values.agent.resources | nindent 10 }}
        securityContext:
          privileged: true
        volumeMounts:
        - name: dev-kvm
          mountPath: /dev/kvm
        - name: socket-dir
          mountPath: /run/imp/sockets
        - name: image-cache
          mountPath: /var/lib/imp/images
      volumes:
      - name: dev-kvm
        hostPath:
          path: /dev/kvm
      - name: socket-dir
        {{- if .Values.agent.hostPaths.socketDir.enabled }}
        hostPath:
          path: {{ .Values.agent.hostPaths.socketDir.path }}
        {{- else }}
        emptyDir: {}
        {{- end }}
      - name: image-cache
        {{- if .Values.agent.hostPaths.imageCache.enabled }}
        hostPath:
          path: {{ .Values.agent.hostPaths.imageCache.path }}
        {{- else }}
        emptyDir: {}
        {{- end }}
      hostPID: false
      hostNetwork: false
```

**Step 4: Run tests**

```bash
helm unittest charts/imp
```

Expected: all tests pass

**Step 5: Commit**

```bash
git add charts/imp/templates/agent/daemonset.yaml charts/imp/tests/agent-daemonset_test.yaml
git commit -m "feat(helm): agent DaemonSet — /dev/kvm, optional hostPaths, NODE_NAME downward API"
```

---

### Task 7: Webhook resources

**Files:**
- Create: `charts/imp/templates/webhook/issuer.yaml`
- Create: `charts/imp/templates/webhook/certificate.yaml`
- Create: `charts/imp/templates/webhook/mutatingwebhook.yaml`
- Create: `charts/imp/templates/webhook/validatingwebhook.yaml`
- Create: `charts/imp/tests/webhook_test.yaml`

Webhook paths are generated by controller-runtime's builder from the API group and kind:
- Mutating ImpVM: `/mutate-imp-dev-v1alpha1-impvm`
- Validating ImpVM: `/validate-imp-dev-v1alpha1-impvm`
- Validating ImpVMClass: `/validate-imp-dev-v1alpha1-impvmclass`
- Validating ImpVMTemplate: `/validate-imp-dev-v1alpha1-impvmtemplate`

The operator Service name is `{{ include "imp.fullname" . }}-operator` in namespace `{{ .Release.Namespace }}`.

**Step 1: Write the failing test** (`charts/imp/tests/webhook_test.yaml`)

```yaml
suite: webhook resources
templates:
  - templates/webhook/issuer.yaml
  - templates/webhook/certificate.yaml
  - templates/webhook/mutatingwebhook.yaml
  - templates/webhook/validatingwebhook.yaml
tests:
  - it: creates Issuer when certManager enabled
    template: templates/webhook/issuer.yaml
    asserts:
      - isKind:
          of: Issuer
      - equal:
          path: metadata.namespace
          value: NAMESPACE

  - it: skips Issuer when custom issuerRef is provided
    template: templates/webhook/issuer.yaml
    set:
      webhook.certManager.issuerRef.name: my-issuer
      webhook.certManager.issuerRef.kind: ClusterIssuer
    asserts:
      - hasDocuments:
          count: 0

  - it: creates Certificate with correct DNS names
    template: templates/webhook/certificate.yaml
    asserts:
      - isKind:
          of: Certificate
      - equal:
          path: spec.secretName
          value: imp-webhook-tls
      - contains:
          path: spec.dnsNames
          content: RELEASE-NAME-imp-operator.NAMESPACE.svc
      - contains:
          path: spec.dnsNames
          content: RELEASE-NAME-imp-operator.NAMESPACE.svc.cluster.local

  - it: MutatingWebhookConfiguration has cainjector annotation
    template: templates/webhook/mutatingwebhook.yaml
    asserts:
      - isKind:
          of: MutatingWebhookConfiguration
      - equal:
          path: metadata.annotations["cert-manager.io/inject-ca-from"]
          value: NAMESPACE/imp-webhook-tls

  - it: ValidatingWebhookConfiguration has cainjector annotation
    template: templates/webhook/validatingwebhook.yaml
    asserts:
      - isKind:
          of: ValidatingWebhookConfiguration
      - equal:
          path: metadata.annotations["cert-manager.io/inject-ca-from"]
          value: NAMESPACE/imp-webhook-tls

  - it: ValidatingWebhookConfiguration has three webhooks
    template: templates/webhook/validatingwebhook.yaml
    asserts:
      - lengthEqual:
          path: webhooks
          count: 3
```

**Step 2: Run test, verify it fails**

```bash
helm unittest charts/imp 2>&1 | tail -5
```

Expected: FAIL

**Step 3: Create `charts/imp/templates/webhook/issuer.yaml`**

```yaml
{{- if and .Values.webhook.certManager.enabled (not .Values.webhook.certManager.issuerRef.name) }}
apiVersion: cert-manager.io/v1
kind: Issuer
metadata:
  name: {{ include "imp.fullname" . }}-selfsigned
  namespace: {{ .Release.Namespace }}
  labels:
    {{- include "imp.labels" . | nindent 4 }}
spec:
  selfSigned: {}
{{- end }}
```

**Step 4: Create `charts/imp/templates/webhook/certificate.yaml`**

```yaml
{{- if .Values.webhook.certManager.enabled }}
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: imp-webhook-tls
  namespace: {{ .Release.Namespace }}
  labels:
    {{- include "imp.labels" . | nindent 4 }}
spec:
  secretName: imp-webhook-tls
  duration: 8760h
  renewBefore: 720h
  dnsNames:
    - {{ include "imp.fullname" . }}-operator.{{ .Release.Namespace }}.svc
    - {{ include "imp.fullname" . }}-operator.{{ .Release.Namespace }}.svc.cluster.local
  issuerRef:
    {{- if .Values.webhook.certManager.issuerRef.name }}
    {{- toYaml .Values.webhook.certManager.issuerRef | nindent 4 }}
    {{- else }}
    name: {{ include "imp.fullname" . }}-selfsigned
    kind: Issuer
    {{- end }}
{{- end }}
```

**Step 5: Create `charts/imp/templates/webhook/mutatingwebhook.yaml`**

```yaml
apiVersion: admissionregistration.k8s.io/v1
kind: MutatingWebhookConfiguration
metadata:
  name: {{ include "imp.fullname" . }}
  labels:
    {{- include "imp.labels" . | nindent 4 }}
  annotations:
    cert-manager.io/inject-ca-from: {{ .Release.Namespace }}/imp-webhook-tls
webhooks:
- name: mimpvm.kb.io
  admissionReviewVersions: ["v1"]
  clientConfig:
    service:
      name: {{ include "imp.fullname" . }}-operator
      namespace: {{ .Release.Namespace }}
      path: /mutate-imp-dev-v1alpha1-impvm
  rules:
  - apiGroups: ["imp.dev"]
    apiVersions: ["v1alpha1"]
    operations: [CREATE, UPDATE]
    resources: [impvms]
  failurePolicy: Fail
  sideEffects: None
```

**Step 6: Create `charts/imp/templates/webhook/validatingwebhook.yaml`**

```yaml
apiVersion: admissionregistration.k8s.io/v1
kind: ValidatingWebhookConfiguration
metadata:
  name: {{ include "imp.fullname" . }}
  labels:
    {{- include "imp.labels" . | nindent 4 }}
  annotations:
    cert-manager.io/inject-ca-from: {{ .Release.Namespace }}/imp-webhook-tls
webhooks:
- name: vimpvm.kb.io
  admissionReviewVersions: ["v1"]
  clientConfig:
    service:
      name: {{ include "imp.fullname" . }}-operator
      namespace: {{ .Release.Namespace }}
      path: /validate-imp-dev-v1alpha1-impvm
  rules:
  - apiGroups: ["imp.dev"]
    apiVersions: ["v1alpha1"]
    operations: [CREATE, UPDATE]
    resources: [impvms]
  failurePolicy: Fail
  sideEffects: None
- name: vimpvmclass.kb.io
  admissionReviewVersions: ["v1"]
  clientConfig:
    service:
      name: {{ include "imp.fullname" . }}-operator
      namespace: {{ .Release.Namespace }}
      path: /validate-imp-dev-v1alpha1-impvmclass
  rules:
  - apiGroups: ["imp.dev"]
    apiVersions: ["v1alpha1"]
    operations: [CREATE, UPDATE]
    resources: [impvmclasses]
  failurePolicy: Fail
  sideEffects: None
- name: vimpvmtemplate.kb.io
  admissionReviewVersions: ["v1"]
  clientConfig:
    service:
      name: {{ include "imp.fullname" . }}-operator
      namespace: {{ .Release.Namespace }}
      path: /validate-imp-dev-v1alpha1-impvmtemplate
  rules:
  - apiGroups: ["imp.dev"]
    apiVersions: ["v1alpha1"]
    operations: [CREATE, UPDATE]
    resources: [impvmtemplates]
  failurePolicy: Fail
  sideEffects: None
```

**Step 7: Run all tests**

```bash
helm unittest charts/imp
helm lint charts/imp-crds charts/imp
```

Expected: all tests pass, 0 chart failures

**Step 8: Final Go test run**

```bash
KUBEBUILDER_ASSETS="$(/Users/giovanni/syscode/git/imp/bin/setup-envtest use --bin-dir /Users/giovanni/syscode/git/imp/bin/k8s -p path 2>/dev/null)" go test ./...
```

Expected: all Go tests still pass (chart work touches no Go code)

**Step 9: Commit**

```bash
git add charts/imp/templates/webhook/ charts/imp/tests/webhook_test.yaml
git commit -m "feat(helm): webhook resources — cert-manager Certificate, Issuer, webhook configs"
```
