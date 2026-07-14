#include <math.h>
#include <pthread.h>
#include <signal.h>
#include <stdio.h>
#include <stdlib.h>
#include <sys/sysctl.h>
#include <time.h>
#include <unistd.h>

static volatile sig_atomic_t keep_running = 1;
static volatile double global_sink = 0.0;

typedef struct {
    int id;
    double seconds;
} worker_arg_t;

static void stop_handler(int sig) {
    (void)sig;
    keep_running = 0;
}

static double now_seconds(void) {
    struct timespec ts;
    clock_gettime(CLOCK_MONOTONIC, &ts);
    return (double)ts.tv_sec + (double)ts.tv_nsec / 1000000000.0;
}

static int logical_cpu_count(void) {
    int count = 0;
    size_t size = sizeof(count);
    if (sysctlbyname("hw.logicalcpu", &count, &size, NULL, 0) == 0 && count > 0) return count;
    long n = sysconf(_SC_NPROCESSORS_ONLN);
    return n > 0 ? (int)n : 4;
}

static void *worker(void *arg) {
    worker_arg_t *w = (worker_arg_t *)arg;
    double end = now_seconds() + w->seconds;
    double x = 0.000001 * (double)(w->id + 1);
    unsigned long long loops = 0;

    while (keep_running && now_seconds() < end) {
        for (int i = 0; i < 300000; i++) {
            x = sin(x + 0.000001) * cos(x + 0.000002) + sqrt(fabs(x) + 1.000001);
            x = fma(x, 1.0000001, 0.0000001);
            if (x > 1000000.0) x *= 0.000001;
        }
        loops++;
    }

    global_sink += x + (double)loops;
    return NULL;
}

int main(int argc, char **argv) {
    signal(SIGINT, stop_handler);
    signal(SIGTERM, stop_handler);

    double seconds = 300.0;
    int threads = logical_cpu_count();

    if (argc > 1) {
        seconds = atof(argv[1]);
        if (seconds <= 0) seconds = 300.0;
    }

    if (argc > 2) {
        int requested = atoi(argv[2]);
        if (requested > 0) threads = requested;
    }

    printf("CPU floating-point stress\n");
    printf("Threads: %d\n", threads);
    printf("Duration: %.0f seconds\n", seconds);
    printf("Press Ctrl+C to stop early.\n");

    pthread_t *tids = calloc((size_t)threads, sizeof(pthread_t));
    worker_arg_t *args = calloc((size_t)threads, sizeof(worker_arg_t));
    if (!tids || !args) {
        fprintf(stderr, "Allocation failed.\n");
        return 1;
    }

    for (int i = 0; i < threads; i++) {
        args[i].id = i;
        args[i].seconds = seconds;
        if (pthread_create(&tids[i], NULL, worker, &args[i]) != 0) {
            fprintf(stderr, "Failed to create thread %d\n", i);
            keep_running = 0;
            threads = i;
            break;
        }
    }

    for (int i = 0; i < threads; i++) pthread_join(tids[i], NULL);

    printf("Done. sink=%f\n", global_sink);
    free(tids);
    free(args);
    return 0;
}
