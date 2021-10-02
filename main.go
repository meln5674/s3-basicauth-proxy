package main

import (
	"flag"
	"fmt"
	"github.com/aws/aws-sdk-go/aws"
	_ "github.com/aws/aws-sdk-go/aws/awserr"
	awscredentials "github.com/aws/aws-sdk-go/aws/credentials"
	_ "github.com/aws/aws-sdk-go/aws/request"
	awssession "github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"io"
	"net/http"
	"os"
	"regexp"
	"time"
)

const (
	dnsNameCharPatternStr           = `[a-z0-9.-]`
	endpointPatternStr              = `[https?:\\` + dnsNameCharPatternStr + `+(:[0-9]+)?`
	bucketNamePatternStr            = dnsNameCharPatternStr + `{3,63}`
	endpointGroupLabel              = `endpoint`
	regionGroupLabel                = `region`
	bucketGroupLabel                = `bucket`
	objectGroupLabel                = `object`
	servicePathPatternStr           = `/(?P<` + endpointGroupLabel + `>` + endpointPatternStr + `)/(?P<` + regionGroupLabel + `>` + dnsNameCharPatternStr + `+)`
	bucketPathPatternStr            = servicePathPatternStr + `/(?P<` + bucketGroupLabel + `>` + bucketNamePatternStr + `)`
	objectPathPatternStr            = bucketPathPatternStr + `/(?P<` + objectGroupLabel + `>.+)`
	objectDirPathPatternStr         = bucketPathPatternStr + `/(?P<` + objectGroupLabel + `>.+/)`
	optionalTrailingSlashPatternStr = `/?`
)

var (
	servicePathPattern            = regexp.MustCompile(ExactRegexp(servicePathPatternStr + optionalTrailingSlashPatternStr))
	bucketPathPattern             = regexp.MustCompile(ExactRegexp(bucketPathPatternStr + optionalTrailingSlashPatternStr))
	objectPathPattern             = regexp.MustCompile(ExactRegexp(objectPathPatternStr))
	objectDirPathPattern          = regexp.MustCompile(ExactRegexp(objectDirPathPatternStr))
	servicePathEndpointGroupIndex = servicePathPattern.SubexpIndex(endpointGroupLabel)
	servicePathRegionGroupIndex   = servicePathPattern.SubexpIndex(regionGroupLabel)

	bucketPathEndpointGroupIndex = bucketPathPattern.SubexpIndex(endpointGroupLabel)
	bucketPathRegionGroupIndex   = bucketPathPattern.SubexpIndex(regionGroupLabel)
	bucketPathBucketGroupIndex   = bucketPathPattern.SubexpIndex(bucketGroupLabel)

	objectDirPathEndpointGroupIndex = objectDirPathPattern.SubexpIndex(endpointGroupLabel)
	objectDirPathRegionGroupIndex   = objectDirPathPattern.SubexpIndex(regionGroupLabel)
	objectDirPathBucketGroupIndex   = objectDirPathPattern.SubexpIndex(bucketGroupLabel)
	objectDirPathObjectGroupIndex   = objectDirPathPattern.SubexpIndex(objectGroupLabel)

	objectPathEndpointGroupIndex = objectPathPattern.SubexpIndex(endpointGroupLabel)
	objectPathRegionGroupIndex   = objectPathPattern.SubexpIndex(regionGroupLabel)
	objectPathBucketGroupIndex   = objectPathPattern.SubexpIndex(bucketGroupLabel)
	objectPathObjectGroupIndex   = objectPathPattern.SubexpIndex(objectGroupLabel)
)

func ExactRegexp(pattern string) string {
	return `^` + pattern + `$`
}

func ParseServicePath(path string) (endpoint, region string, matched bool) {
	match := servicePathPattern.FindStringSubmatch(path)
	if match == nil {
		return "", "", false
	}
	return match[servicePathEndpointGroupIndex],
		match[servicePathRegionGroupIndex],
		true
}

func ParseBucketPath(path string) (endpoint, region, bucket string, matched bool) {
	match := bucketPathPattern.FindStringSubmatch(path)
	if match == nil {
		return "", "", "", false
	}
	return match[bucketPathEndpointGroupIndex],
		match[bucketPathRegionGroupIndex],
		match[bucketPathBucketGroupIndex],
		true
}

func ParseObjectPath(path string) (endpoint, region, bucket, object string, matched bool) {
	match := objectPathPattern.FindStringSubmatch(path)
	if match == nil {
		return "", "", "", "", false
	}
	return match[objectPathEndpointGroupIndex],
		match[objectPathRegionGroupIndex],
		match[objectPathBucketGroupIndex],
		match[objectPathObjectGroupIndex],
		true
}

func ParseObjectDirPath(path string) (endpoint, region, bucket, prefix string, matched bool) {
	match := objectDirPathPattern.FindStringSubmatch(path)
	if match == nil {
		return "", "", "", "", false
	}
	return match[objectDirPathEndpointGroupIndex],
		match[objectDirPathRegionGroupIndex],
		match[objectDirPathBucketGroupIndex],
		match[objectDirPathObjectGroupIndex],
		true
}

type Proxy struct {
	ListenAddr  string
	ListenPort  int
	TLSKeyPath  string
	TLSCertPath string
	awsSession  awssession.Session
}

func (p *Proxy) AddFlags(flags *flag.FlagSet) {
	flags.StringVar(&p.ListenAddr, "listen-addr", "127.0.0.1", "Address to listen on")
	flags.IntVar(&p.ListenPort, "listen-port", 8080, "Port to listen on")
	flags.StringVar(&p.TLSKeyPath, "tls-key", "", "Path to key to sign TLS requests with. Will use https if provided, http if omitted")
	flags.StringVar(&p.TLSCertPath, "tls-cert", "", "Path to certificate to provide over https. Required if -tls-key is provided, must be blank otherwise")
}

type credentials struct {
	accessKey string
	secretKey string
}

type serviceForward struct {
	s3 *s3.S3
}

func makeServiceForward(endpoint, region string, creds *credentials) (serviceForward, error) {
	var awscreds *awscredentials.Credentials
	if creds != nil {
		awscreds = awscredentials.NewCredentials(&awscredentials.StaticProvider{
			Value: awscredentials.Value{
				AccessKeyID:     creds.accessKey,
				SecretAccessKey: creds.secretKey,
			},
		})
	}
	// TODO: Cache clients
	config := &aws.Config{
		Endpoint:         &endpoint,
		Region:           &region,
		Credentials:      awscreds,
		S3ForcePathStyle: aws.Bool(true),
	}
	session, err := awssession.NewSession(config)
	if err != nil {
		return serviceForward{}, err
	}
	return serviceForward{s3: s3.New(session, config)}, nil
}

func indexHeader() string {
	return "Name\tSize\tDate\n" // TODO: HTML Table
}

func indexLine(name string, size *int64, date time.Time) string {
	// TODO: HTML Table w/ links
	if size != nil {
		return fmt.Sprintf("%s\t%d\t%s\n", name, *size, date)
	} else {
		return fmt.Sprintf("%s\t---\t%s\n", name, date)
	}
}

func processError(resp http.ResponseWriter, err error) {
	http.Error(resp, err.Error(), http.StatusInternalServerError) // TODO: Handle common errors like not found, unauthorized, etc
}

func (s *serviceForward) HeadIndex(resp http.ResponseWriter) {
	// TODO: Compute size of index and set Content-Length
}

func (s *serviceForward) Index(resp http.ResponseWriter) {
	out, err := s.s3.ListBuckets(&s3.ListBucketsInput{})
	if err != nil {
		processError(resp, err)
		return
	}
	// TODO: Handle pagination
	resp.Write([]byte(indexHeader()))
	for _, bucket := range out.Buckets {
		resp.Write([]byte(indexLine(*bucket.Name, nil, *bucket.CreationDate)))
	}
}

type bucketForward struct {
	serviceForward
	bucket string
}

func (b *bucketForward) HeadIndex(resp http.ResponseWriter) {
	// TODO: Compute size of index and set Content-Length
}

func (b *bucketForward) Index(resp http.ResponseWriter) {
	out, err := b.s3.ListObjects(&s3.ListObjectsInput{
		Bucket: &b.bucket,
	})
	if err != nil {
		processError(resp, err)
		return
	}
	resp.Write([]byte(indexHeader()))
	// TODO: Show common prefixes with a single / at the end
	// TODO: Only show objects without a / in them
	// TODO: Handle pagination
	for _, object := range out.Contents {
		resp.Write([]byte(indexLine(*object.Key, object.Size, *object.LastModified)))
	}
}

func (b *bucketForward) Create(resp http.ResponseWriter) {
	// TODO: Configurable default bucket settings
	_, err := b.s3.CreateBucket(&s3.CreateBucketInput{
		Bucket: &b.bucket,
	})
	if err != nil {
		processError(resp, err)
		return
	}
}

func (b *bucketForward) Delete(resp http.ResponseWriter) {
	_, err := b.s3.DeleteBucket(&s3.DeleteBucketInput{
		Bucket: &b.bucket,
	})
	if err != nil {
		processError(resp, err)
		return
	}
}

type objectForward struct {
	bucketForward
	object string
}

func (o *objectForward) HeadDownload(resp http.ResponseWriter) {
	out, err := o.s3.GetObject(&s3.GetObjectInput{
		Bucket: &o.bucket,
		Key:    &o.object,
	})
	if err != nil {
		processError(resp, err)
		return
	}
	// TODO: Add other headers
	resp.Header().Set("Content-Length", fmt.Sprintf("%d", *out.ContentLength))
}

func (o *objectForward) Download(resp http.ResponseWriter) {
	out, err := o.s3.GetObject(&s3.GetObjectInput{
		Bucket: &o.bucket,
		Key:    &o.object,
	})
	if err != nil {
		processError(resp, err)
		return
	}
	// TODO: Add headers
	if _, err := io.Copy(resp, out.Body); err != nil {
		// TODO: Log error???
	}
}

func (o *objectForward) Upload(resp http.ResponseWriter, contents io.ReadCloser) {
	// TODO: Configurable default settings
	_, err := o.s3.PutObject(&s3.PutObjectInput{
		Bucket: &o.bucket,
		Key:    &o.object,
		Body:   aws.ReadSeekCloser(contents),
	})
	if err != nil {
		processError(resp, err)
		return
	}
}

func (o *objectForward) Delete(resp http.ResponseWriter) {
	_, err := o.s3.DeleteObject(&s3.DeleteObjectInput{
		Bucket: &o.bucket,
		Key:    &o.object,
	})
	if err != nil {
		processError(resp, err)
		return
	}
}

type objectPrefixForward struct {
	bucketForward
	prefix string
}

func (o *objectPrefixForward) HeadIndex(resp http.ResponseWriter) {
	// TODO: Compute size of index, set Content-Length
}

func (o *objectPrefixForward) Index(resp http.ResponseWriter) {
	out, err := o.s3.ListObjects(&s3.ListObjectsInput{
		Bucket: &o.bucket,
		Prefix: &o.prefix,
	})
	if err != nil {
		processError(resp, err)
		return
	}
	resp.Write([]byte(indexHeader()))
	// TODO: Handle pagination
	// TODO: Show common prefixes with a single / at the end
	// TODO: Only show objects without a / in them
	for _, object := range out.Contents {
		resp.Write([]byte(indexLine(*object.Key, object.Size, *object.LastModified)))
	}

}

func (o *objectPrefixForward) Delete(resp http.ResponseWriter) {
	objsOut, err := o.s3.ListObjects(&s3.ListObjectsInput{
		Bucket: &o.bucket,
		Prefix: &o.prefix,
	})
	if err != nil {
		processError(resp, err)
		return
	}
	// TODO: Handle pagination
	objIds := make([]*s3.ObjectIdentifier, len(objsOut.Contents))
	for ix := range objsOut.Contents {
		objIds[ix] = &s3.ObjectIdentifier{Key: objsOut.Contents[ix].Key}
	}
	_, err = o.s3.DeleteObjects(&s3.DeleteObjectsInput{
		Bucket: &o.bucket,
		Delete: &s3.Delete{
			Objects: objIds,
		},
	})
}

func (p *Proxy) ServeHTTP(resp http.ResponseWriter, req *http.Request) {
	var creds *credentials
	if username, password, hasCreds := req.BasicAuth(); hasCreds {
		creds = &credentials{
			accessKey: username,
			secretKey: password,
		}
	}
	if req.URL.Path == "/" {
		resp.Write([]byte("Proxy is running"))
		return
	}
	if endpoint, region, matched := ParseServicePath(req.URL.Path); matched {
		svc, err := makeServiceForward(endpoint, region, creds)
		if err != nil {
			http.Error(resp, err.Error(), http.StatusInternalServerError)
			return
		}
		switch req.Method {
		case http.MethodHead:
			svc.HeadIndex(resp)
		case http.MethodGet:
			svc.Index(resp)
		default:
			http.Error(resp, "Method not allowed", http.StatusMethodNotAllowed)
		}
		return
	}
	if endpoint, region, bucket, matched := ParseBucketPath(req.URL.Path); matched {
		svc, err := makeServiceForward(endpoint, region, creds)
		if err != nil {
			http.Error(resp, err.Error(), http.StatusInternalServerError)
			return
		}
		bkt := bucketForward{serviceForward: svc, bucket: bucket}
		switch req.Method {
		case http.MethodHead:
			bkt.HeadIndex(resp)
		case http.MethodGet:
			bkt.Index(resp)
		case http.MethodPut:
		case http.MethodPost:
			bkt.Create(resp)
		case http.MethodDelete:
			bkt.Delete(resp)
		default:
			http.Error(resp, "Method not allowed", http.StatusMethodNotAllowed)
		}
		return
	}
	if endpoint, region, bucket, object, matched := ParseObjectPath(req.URL.Path); matched {
		svc, err := makeServiceForward(endpoint, region, creds)
		if err != nil {
			http.Error(resp, err.Error(), http.StatusInternalServerError)
			return
		}
		obj := objectForward{bucketForward: bucketForward{serviceForward: svc, bucket: bucket}, object: object}

		switch req.Method {
		case http.MethodHead:
			obj.HeadDownload(resp)
		case http.MethodGet:
			obj.Download(resp)
		case http.MethodPut:
		case http.MethodPost:
			obj.Upload(resp, req.Body)
		case http.MethodDelete:
			obj.Delete(resp)
		default:
			http.Error(resp, "Method not allowed", http.StatusMethodNotAllowed)
		}
	}
	if endpoint, region, bucket, prefix, matched := ParseObjectDirPath(req.URL.Path); matched {
		svc, err := makeServiceForward(endpoint, region, creds)
		if err != nil {
			http.Error(resp, err.Error(), http.StatusInternalServerError)
			return
		}
		obj := objectPrefixForward{bucketForward: bucketForward{serviceForward: svc, bucket: bucket}, prefix: prefix}

		switch req.Method {
		case http.MethodHead:
			obj.HeadIndex(resp)
		case http.MethodGet:
			obj.Index(resp)
		case http.MethodDelete:
			obj.Delete(resp)
		default:
			http.Error(resp, "Method not allowed", http.StatusMethodNotAllowed)
		}
		return
	}
	http.Error(resp, "Not Found", http.StatusNotFound)
}

func (p *Proxy) ListenAndServe() error {
	addr := fmt.Sprintf("%s:%d", p.ListenAddr, p.ListenPort)
	fmt.Printf("Running on %s...\n", addr)
	if p.TLSKeyPath != "" {
		return http.ListenAndServeTLS(
			addr,
			p.TLSKeyPath,
			p.TLSCertPath,
			p,
		)
	} else {
		return http.ListenAndServe(addr, p)
	}
}

func main() {
	proxy := Proxy{}
	proxy.AddFlags(flag.CommandLine)
	flag.Parse()
	err := proxy.ListenAndServe()
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
