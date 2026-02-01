import boto3

def create_s3_bucket():
    s3 = boto3.client('s3')
    s3.create_bucket(Bucket='my-bucket')
    s3.put_object(Bucket='my-bucket', Key='test.txt', Body='Hello World')
