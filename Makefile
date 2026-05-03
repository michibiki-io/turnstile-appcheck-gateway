.PHONY: compose-e2e-mock compose-e2e-down e2e-kind-mock e2e-kind-keep e2e-kind-clean e2e-real-local act-release-e2e

compose-e2e-mock:
	cp -n dev/.env.example dev/.env || true
	chmod +x scripts/e2e-smoke-mock.sh
	docker compose --env-file dev/.env -f dev/docker-compose.yml -f dev/docker-compose.e2e.yml up -d --build
	./scripts/e2e-smoke-mock.sh "http://127.0.0.1:$$(awk -F= '/^TRAEFIK_PORT=/{print $$2; found=1} END{if(!found) print 8080}' dev/.env)"

compose-e2e-down:
	docker compose --env-file dev/.env -f dev/docker-compose.yml -f dev/docker-compose.e2e.yml down -v

e2e-kind-mock:
	chmod +x scripts/helm-e2e-kind.sh scripts/e2e-smoke-mock.sh
	./scripts/helm-e2e-kind.sh

e2e-kind-keep:
	chmod +x scripts/helm-e2e-kind.sh scripts/e2e-smoke-mock.sh
	KEEP_CLUSTER=true ./scripts/helm-e2e-kind.sh

e2e-kind-clean:
	kind delete cluster --name turnstile-appcheck-gateway-e2e || true
	rm -f .tmp/kind-turnstile-appcheck-gateway-e2e.kubeconfig || true

e2e-real-local:
	chmod +x scripts/e2e-real-local.sh
	./scripts/e2e-real-local.sh

act-release-e2e:
	chmod +x scripts/act-release-e2e.sh
	./scripts/act-release-e2e.sh
