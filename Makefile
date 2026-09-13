BINAIRE  := parallax
VERSION  := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS  := -s -w -X main.version=$(VERSION)

# CGO désactivé : le pilote SQLite est en Go pur. Le binaire compilé sous
# WSL2 tourne tel quel sur RHEL 8 ou 9, sans dépendance à la glibc locale.
export CGO_ENABLED := 0

.DEFAULT_GOAL := check

## check : la seule commande à lancer — dépendances, compilation, vet, tests
.PHONY: check
check: spec tidy build vet test
	@echo
	@echo "OK — build, vet et tests passés"

.PHONY: tidy
tidy:
	@echo "==> go mod tidy"
	@go mod tidy

.PHONY: build
build:
	@echo "==> go build"
	@go build -ldflags "$(LDFLAGS)" ./...

.PHONY: vet
vet:
	@echo "==> go vet"
	@go vet ./...

.PHONY: test
test:
	@echo "==> go test"
	@go test ./...

## spec : rejoue la spécification exécutable et régénère les vecteurs.
## Ignorée si Python est absent : vectors.json est versionné.
.PHONY: spec
spec:
	@if command -v python3 >/dev/null 2>&1; then \
		echo "==> spécification exécutable"; \
		python3 spec/validate_schema.py > /dev/null || { python3 spec/validate_schema.py; exit 1; }; \
		python3 spec/capacity_reference.py > /dev/null; \
		echo "    schéma validé, vecteurs à jour"; \
	else \
		echo "==> python3 absent, spécification non rejouée (vectors.json versionné)"; \
	fi

## spec-check : échoue si vectors.json diverge de la spécification
.PHONY: spec-check
spec-check:
	@python3 spec/capacity_reference.py --check

## release : binaire optimisé pour la cible de déploiement (RHEL)
.PHONY: release
release:
	@GOOS=linux GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o bin/$(BINAIRE) ./cmd/parallax
	@echo "bin/$(BINAIRE) construit ($(VERSION))"

## release-macos-arm64 : binaire pour un Mac Apple Silicon — poste de travail
## pour tester/saisir en local, pas la cible de déploiement (qui reste RHEL,
## voir release). Même CGO_ENABLED=0 et pilote SQLite pur Go : aucune
## dépendance à installer sur le Mac non plus.
.PHONY: release-macos-arm64
release-macos-arm64:
	@GOOS=darwin GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o bin/$(BINAIRE)-macos-arm64 ./cmd/parallax
	@echo "bin/$(BINAIRE)-macos-arm64 construit ($(VERSION))"

## release-windows-amd64 : binaire pour un poste Windows — même raison que
## release-macos-arm64, pas une cible de déploiement.
.PHONY: release-windows-amd64
release-windows-amd64:
	@GOOS=windows GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o bin/$(BINAIRE)-windows-amd64.exe ./cmd/parallax
	@echo "bin/$(BINAIRE)-windows-amd64.exe construit ($(VERSION))"

## release-tous : les trois binaires de poste de travail plus la cible RHEL
.PHONY: release-tous
release-tous: release release-windows-amd64 release-macos-arm64

## run : lance l'application sur une base locale
.PHONY: run
run: build
	@go run ./cmd/parallax -base ./data/parallax.db

.PHONY: clean
clean:
	@rm -rf bin/ data/parallax.db*
	@go clean -testcache

.PHONY: aide
aide:
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/## /  /'
