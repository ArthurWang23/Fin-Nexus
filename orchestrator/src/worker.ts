import { Worker, NativeConnection } from '@temporalio/worker';
import * as path from 'path';

async function run() {
    const temporalAddress = process.env.TEMPORAL_CLI_ADDRESS || 'localhost:7233';
    const taskQueue = 'graph-task-queue';
    console.log(`[TS Worker] Connecting to Temporal Server at ${temporalAddress}...`);
    // Step 1: Register Workflows and Activities with the Worker and connect to
    // the Temporal server.
    let connection;
    try {
        connection = await NativeConnection.connect({
            address: temporalAddress,
            // 如果 Temporal 开启了 mTLS，这里需要配置 clientCert 和 clientKey
        });
    } catch (err) {
        console.error(`[TS Worker] Failed to connect to Temporal. Is the server running?`, err);
        process.exit(1);
    }
    const worker = await Worker.create({
        connection,
        namespace: 'default',
        taskQueue: taskQueue,
        workflowsPath: require.resolve('./workflows'),
        // 不需要 activity 参数，因为都在 Go 实现
    });
    // Worker connects to localhost by default and uses console.error for logging.
    // Customize the Worker by passing more options to create().

    // Step 2: Start accepting tasks on the Task Queue.
    //
    // The worker runs until it encounters an unexpected error or the process is
    // killed.
    console.log(`[TS Worker] Started! Listening on queue: '${taskQueue}'`);
    console.log(`[TS Worker] It will delegate activities to Go workers on 'agent-task-queue'`);
    await worker.run();
}

run().catch((err) => {
    console.error('[TS Worker] Fatal error:', err);
    process.exit(1);
});
