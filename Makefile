VERSION := 1.0.0
.PHONY: all test vet build cross clean package
all: test build
test:
	./scripts/run_tests.sh
vet:
	go vet ./...
build:
	go build -trimpath -ldflags "-s -w" -o bin/macpowerlab ./cmd/macpowerlab
cross:
	./scripts/build_go.sh
package:
	./scripts/package_release.sh
clean:
	rm -rf bin/macpowerlab bin/native dist/MacPowerLab.app dist/*.zip dist/*.tar.gz
