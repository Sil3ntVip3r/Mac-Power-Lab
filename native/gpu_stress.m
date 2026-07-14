#import <Foundation/Foundation.h>
#import <Metal/Metal.h>
#include <signal.h>
#include <stdlib.h>
#include <stdio.h>
#include <string.h>

static volatile sig_atomic_t keepRunning = 1;

void stopHandler(int sig) {
    keepRunning = 0;
}

typedef struct {
    int inflight;
    int bufferMB;
    int shaderLoops;
} Profile;

static Profile profileForName(const char *name) {
    // v0.6.3 responsive profiles: smaller command chunks so requested
    // durations are respected more closely and the UI does not sit at 100%
    // while a giant Metal command buffer finishes.
    if (name && strcmp(name, "extreme") == 0) return (Profile){4, 256, 24576};
    if (name && strcmp(name, "high") == 0) return (Profile){3, 192, 16384};
    return (Profile){2, 128, 8192};
}

int main(int argc, char **argv) {
    @autoreleasepool {
        double seconds = 300.0;
        const char *profileName = "high";

        if (argc > 1) {
            seconds = atof(argv[1]);
            if (seconds <= 0) seconds = 300.0;
        }

        if (argc > 2) profileName = argv[2];

        Profile p = profileForName(profileName);

        if (argc > 3) {
            int overrideMB = atoi(argv[3]);
            if (overrideMB >= 64 && overrideMB <= 2048) p.bufferMB = overrideMB;
        }

        signal(SIGINT, stopHandler);
        signal(SIGTERM, stopHandler);

        id<MTLDevice> device = MTLCreateSystemDefaultDevice();
        if (!device) {
            fprintf(stderr, "No Metal GPU device found.\n");
            return 1;
        }

        NSUInteger elements = ((NSUInteger)p.bufferMB * 1024 * 1024) / (sizeof(float) * 4);

        printf("Metal GPU stress\n");
        printf("Using GPU: %s\n", [[device name] UTF8String]);
        printf("Profile: %s\n", profileName);
        printf("Duration: %.0f seconds\n", seconds);
        printf("Inflight command buffers: %d\n", p.inflight);
        printf("Buffer size: %d MB each\n", p.bufferMB);
        printf("Shader loops: %d\n", p.shaderLoops);
        printf("Press Ctrl+C to stop early.\n");
        fflush(stdout);

        NSString *source = [NSString stringWithFormat:
        @"#include <metal_stdlib>\n"
        @"using namespace metal;\n"
        @"kernel void burn(device float4 *out [[buffer(0)]], uint id [[thread_position_in_grid]]) {\n"
        @"    float4 x = float4((id %% 4096) + 1) * 0.000244140625;\n"
        @"    for (uint i = 0; i < %d; i++) {\n"
        @"        x = sin(x * 1.0001 + 0.001) + cos(x * 0.9999 + 0.002);\n"
        @"        x = fma(x, 1.00001, 0.00001);\n"
        @"    }\n"
        @"    out[id] = x;\n"
        @"}\n", p.shaderLoops];

        NSError *error = nil;
        id<MTLLibrary> library = [device newLibraryWithSource:source options:nil error:&error];
        if (!library) {
            fprintf(stderr, "Metal shader compile failed: %s\n", [[error localizedDescription] UTF8String]);
            return 1;
        }

        id<MTLFunction> function = [library newFunctionWithName:@"burn"];
        id<MTLComputePipelineState> pipeline = [device newComputePipelineStateWithFunction:function error:&error];
        if (!pipeline) {
            fprintf(stderr, "Metal pipeline failed: %s\n", [[error localizedDescription] UTF8String]);
            return 1;
        }

        id<MTLCommandQueue> queue = [device newCommandQueue];

        NSMutableArray *buffers = [NSMutableArray array];
        for (int i = 0; i < p.inflight; i++) {
            id<MTLBuffer> buffer = [device newBufferWithLength:elements * sizeof(float) * 4
                                                       options:MTLResourceStorageModeShared];
            if (!buffer) {
                fprintf(stderr, "Could not allocate Metal buffer.\n");
                return 1;
            }
            [buffers addObject:buffer];
        }

        MTLSize grid = MTLSizeMake(elements, 1, 1);
        NSUInteger threads = pipeline.maxTotalThreadsPerThreadgroup;
        if (threads > 256) threads = 256;
        MTLSize threadgroup = MTLSizeMake(threads, 1, 1);

        NSDate *end = [NSDate dateWithTimeIntervalSinceNow:seconds];
        unsigned long long loops = 0;

        while (keepRunning && [[NSDate date] compare:end] == NSOrderedAscending) {
            @autoreleasepool {
                NSMutableArray *commands = [NSMutableArray array];

                for (int i = 0; i < p.inflight; i++) {
                    id<MTLCommandBuffer> commandBuffer = [queue commandBuffer];
                    id<MTLComputeCommandEncoder> encoder = [commandBuffer computeCommandEncoder];

                    [encoder setComputePipelineState:pipeline];
                    [encoder setBuffer:[buffers objectAtIndex:i] offset:0 atIndex:0];
                    [encoder dispatchThreads:grid threadsPerThreadgroup:threadgroup];
                    [encoder endEncoding];

                    [commandBuffer commit];
                    [commands addObject:commandBuffer];
                }

                for (id<MTLCommandBuffer> commandBuffer in commands) {
                    [commandBuffer waitUntilCompleted];
                    if ([commandBuffer error]) {
                        fprintf(stderr, "GPU command error: %s\n", [[commandBuffer.error localizedDescription] UTF8String]);
                        keepRunning = 0;
                        break;
                    }
                }
            }

            loops += (unsigned long long)p.inflight;
            printf("\rGPU command buffers completed: %llu", loops);
            fflush(stdout);
        }

        printf("\nDone. GPU command buffers completed: %llu\n", loops);
        return 0;
    }
}
