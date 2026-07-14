#include <signal.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/sysctl.h>
#include <time.h>
#include <unistd.h>

static volatile sig_atomic_t keep_running = 1;
static volatile uint64_t checksum_sink = 0;

static void stop_handler(int sig) {
    (void)sig;
    keep_running = 0;
}

static double now_seconds(void) {
    struct timespec ts;
    clock_gettime(CLOCK_MONOTONIC, &ts);
    return (double)ts.tv_sec + (double)ts.tv_nsec / 1000000000.0;
}

static uint64_t total_memory_bytes(void) {
    uint64_t mem = 0;
    size_t size = sizeof(mem);
    if (sysctlbyname("hw.memsize", &mem, &size, NULL, 0) == 0 && mem > 0) return mem;
    return 8ULL * 1024ULL * 1024ULL * 1024ULL;
}

int main(int argc, char **argv) {
    signal(SIGINT, stop_handler);
    signal(SIGTERM, stop_handler);

    double seconds = 300.0;
    uint64_t total_mem = total_memory_bytes();
    uint64_t default_mb = total_mem / 1024ULL / 1024ULL / 4ULL;

    if (default_mb > 24576) default_mb = 24576;
    if (default_mb < 512) default_mb = 512;

    uint64_t mb = default_mb;

    if (argc > 1) {
        seconds = atof(argv[1]);
        if (seconds <= 0) seconds = 300.0;
    }

    if (argc > 2) {
        uint64_t requested = strtoull(argv[2], NULL, 10);
        if (requested >= 128) mb = requested;
    }

    uint64_t bytes = mb * 1024ULL * 1024ULL;

    printf("Memory bandwidth stress - no disk writes\n");
    printf("Memory block: %llu MB\n", (unsigned long long)mb);
    printf("Duration: %.0f seconds\n", seconds);
    printf("Press Ctrl+C to stop early.\n");

    uint8_t *buf = NULL;
    if (posix_memalign((void **)&buf, 4096, (size_t)bytes) != 0 || !buf) {
        fprintf(stderr, "Could not allocate memory block.\n");
        return 1;
    }

    memset(buf, 0xA5, (size_t)bytes);

    double end = now_seconds() + seconds;
    double last_report = now_seconds();
    uint64_t passes = 0;
    uint64_t total_bytes_touched = 0;

    while (keep_running && now_seconds() < end) {
        for (uint64_t i = 0; i < bytes; i += 64) {
            buf[i] = (uint8_t)((buf[i] + i + passes) & 0xFF);
            checksum_sink += buf[i];
        }

        for (uint64_t i = 32; i < bytes; i += 64) checksum_sink += buf[i];

        passes++;
        total_bytes_touched += bytes * 2ULL;

        double now = now_seconds();
        if (now - last_report >= 1.0) {
            double gb = (double)total_bytes_touched / 1000000000.0;
            printf("\rPasses: %llu  touched: %.1f GB", (unsigned long long)passes, gb);
            fflush(stdout);
            last_report = now;
        }
    }

    printf("\nDone. checksum=%llu\n", (unsigned long long)checksum_sink);
    free(buf);
    return 0;
}
