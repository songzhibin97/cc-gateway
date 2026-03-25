.PHONY: build frontend backend clean dev-frontend dev-backend

build: frontend backend

frontend:
	cd web && npm install && npm run build

backend:
	mkdir -p web/dist
	test -f web/dist/index.html || printf '%s\n' '<!DOCTYPE html><html lang="en"><body>Frontend assets are not built. Run <code>cd web && npm run build</code>.</body></html>' > web/dist/index.html
	go build -o cc-gateway ./cmd/gateway

clean:
	rm -rf cc-gateway web/dist web/node_modules

dev-frontend:
	cd web && npm run dev

dev-backend:
	go run ./cmd/gateway --config config.yaml
