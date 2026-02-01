const AWS = require('aws-sdk');

async function createBucket() {
    const s3 = new AWS.S3();
    await s3.createBucket({
        Bucket: 'my-bucket'
    }).promise();
}
