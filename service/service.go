package service

import (
  "fmt"
  "log"
  "io/ioutil"
  "text/template"
  "bytes"
  "strings"

  "github.com/lann/builder"
  kubernetes "k8s.io/client-go/kubernetes"
	v1 "k8s.io/client-go/pkg/api/v1"
	v1beta1 "k8s.io/client-go/pkg/apis/extensions/v1beta1"
  watch "k8s.io/client-go/pkg/watch"

  yaml "github.com/ghodss/yaml"
)

const namespaceTemplateName = "NamespaceTemplate"
const deploymentTemplateName = "DeploymentTemplate"
const serviceTemplateName = "ServiceTemplate"
const ingressTemplateName = "IngressTemplate"
const fireflyPrefix = "firefly"
const fireflyShadowPrefix = "fireflyshadow"

// Service is a struct
type Service struct {
  // These are Go tempates files
  NamespaceYAMLFile string
  DeploymentYAMLFile string
  ServiceYAMLFile string
  IngressYAMLFile string
  IngressShadowYAMLFile string

  // These are actual templates
  namespaceTemplate *template.Template
  deploymentTemplate *template.Template
  serviceTemplate *template.Template
  ingressTemplate *template.Template
  ingressShadowTemplate *template.Template

  IngressControllerImage string
  MaxDepth int // This is how many TOKENS to allow, we create D+1 namespaces
  Namespace string

  KubernetesClientSet kubernetes.Clientset
}

type megaData struct {
  // Namespace Specific
  Name string
  Namespace string

  // Deployment Specific
  Selector string
  IngressControllerImage string
  ContainerName string
  WatchNamespace string

  // Serivce Specifc
  ServiceName string
  ServicePort string
  TypeString string

  // Ingress Specific
  FireflyPath string
}

type serviceBuilder builder.Builder

func (b serviceBuilder) NamespaceYAMLFile(f string) serviceBuilder {
  return builder.Set(b, "NamespaceYAMLFile", f).(serviceBuilder)
}
func (b serviceBuilder) DeploymentYAMLFile(f string) serviceBuilder {
    return builder.Set(b, "DeploymentYAMLFile", f).(serviceBuilder)
}

func (b serviceBuilder) ServiceYAMLFile(f string) serviceBuilder {
    return builder.Set(b, "ServiceYAMLFile", f).(serviceBuilder)
}

func (b serviceBuilder) IngressYAMLFile(f string) serviceBuilder {
    return builder.Set(b, "IngressYAMLFile", f).(serviceBuilder)
}

func (b serviceBuilder) IngressShadowYAMLFile(f string) serviceBuilder {
    return builder.Set(b, "IngressShadowYAMLFile", f).(serviceBuilder)
}

func (b serviceBuilder) IngressControllerImage(i string) serviceBuilder {
    return builder.Set(b, "IngressControllerImage", i).(serviceBuilder)
}

func (b serviceBuilder) MaxDepth(d int) serviceBuilder {
    return builder.Set(b, "MaxDepth", d).(serviceBuilder)
}

func (b serviceBuilder) Namespace(n string) serviceBuilder {
    return builder.Set(b, "Namespace", n).(serviceBuilder)
}

func (b serviceBuilder) KubernetesClientSet(cs kubernetes.Clientset) serviceBuilder {
    return builder.Set(b, "KubernetesClientSet", cs).(serviceBuilder)
}

func (b serviceBuilder) Build() Service {
  s := builder.GetStruct(b).(Service)

  namespaceTemplate, err := loadTemplate(s.NamespaceYAMLFile, namespaceTemplateName)
  if err != nil {
    log.Fatalf("Could not open DeploymentYAML file (%s). Exiting application", s.DeploymentYAMLFile)
  }
  s.namespaceTemplate = namespaceTemplate

  deploymentTemplate, err := loadTemplate(s.DeploymentYAMLFile, deploymentTemplateName)
  if err != nil {
    log.Fatalf("Could not open DeploymentYAML file (%s). Exiting application", s.DeploymentYAMLFile)
  }
  s.deploymentTemplate = deploymentTemplate

  serviceTemplate, err := loadTemplate(s.ServiceYAMLFile, serviceTemplateName)
  if err != nil {
    log.Fatalf("Could not open ServiceYAML file (%s). Exiting application", s.ServiceYAMLFile)
  }
  s.serviceTemplate = serviceTemplate

  ingressTemplate, err := loadTemplate(s.IngressYAMLFile, ingressTemplateName)
  if err != nil {
    log.Fatalf("Could not open IngressYAML file (%s). Exiting application", s.IngressYAMLFile)
  }
  s.ingressTemplate = ingressTemplate

  ingressShadowTemplate, err := loadTemplate(s.IngressShadowYAMLFile, ingressTemplateName)
  if err != nil {
    log.Fatalf("Could not open IngressShadowYAML file (%s). Exiting application", s.IngressShadowYAMLFile)
  }
  s.ingressShadowTemplate = ingressShadowTemplate

  return s
}

func getDefaultName(i int) (string){
  return fmt.Sprintf("%s%d", fireflyPrefix, i)
}

func getDefaultShadowName(i int) (string){
  return fmt.Sprintf("%s%d", fireflyShadowPrefix, i)
}

// template location, template name
func loadTemplate (s string, n string) (t *template.Template, err error) {
  b, err := ioutil.ReadFile(s)
  if err != nil {
    return t, err
  }
  t = template.New(n)
	t, err = t.Parse(string(b))
  if err != nil {
    return t, err
  }
  return t, nil
}

// Watch is an infitite watch of a particular namespace's Ingress rules
func (s Service) Watch() () {
  i := s.KubernetesClientSet.Ingresses(s.Namespace)

	watchInterface, err := i.Watch(v1.ListOptions{})
	if err != nil {
    log.Fatal(err)
		return
	}

  s.deployScaffolding()

  log.Print("Watching channel")
	c := watchInterface.ResultChan()

	go func() {
		for {
			e := <-c
			log.Printf("%v", e.Type)
      if ing, ok := e.Object.(*v1beta1.Ingress); ok {
        if ing.ObjectMeta.Labels["firefly.optin"] != "" {
          log.Print("Found an opt-in service to register.")
          switch e.Type {
          case watch.Added:
            s.createOrUpdate(ing)
          case watch.Modified:
            s.createOrUpdate(ing)
          case watch.Deleted:
            log.Printf("TODO: add another service for cleanup")
          }
        }
      }
		}
	}()

  select {}
}

func (s Service) createOrUpdate(ing *v1beta1.Ingress) () {
  for _, rule := range ing.Spec.Rules {
    for _, path := range rule.HTTP.Paths {

      log.Printf("Found an HTTP Path (%s)", path.Path)
      paths := strings.SplitN(strings.Trim(path.Path,"/"), "/", s.MaxDepth)
      if paths != nil {
        for i, pathToken := range paths {
          log.Printf("Token %d: %s", i, pathToken)
          if i >= (len(paths) - 1) {
            // Create shadow Controller
            s.createShadowComponents(i+1, pathToken, &path)
          } else {
            s.createOrUpdateIngressFromToken(i+1, pathToken, "")
          }
        }
      } else {
        s.createShadowComponents(1, path.Path, &path)
      }

    }
  }
  return
}

func (s Service) createShadowComponents(i int, t string, path *v1beta1.HTTPIngressPath) (err error) {
  name := getDefaultName(i)
  shadowName := getDefaultShadowName(i)

  m := megaData {
    // Namespace Specifc
    Name: path.Backend.ServiceName,
    Namespace: name,
    // Deployment Specific
    Selector: shadowName,
    IngressControllerImage: s.IngressControllerImage,
    ContainerName: shadowName,
    WatchNamespace: s.Namespace,
    // Service Specifc
    ServiceName: path.Backend.ServiceName,
    TypeString: "type: NodePort",
  }
  d := v1beta1.Deployment{}
  err = templateToResource(*s.deploymentTemplate, m, &d)
  if err != nil {
    log.Fatalf("Error templating deployment: %v", err)
    return
  }
  log.Printf("Creating deployment %s", d.Name)
  _, err = s.KubernetesClientSet.Deployments(name).Create(&d)
  if err != nil {
    log.Printf("Error creating deployment: %v", err)
    _, err = s.KubernetesClientSet.Deployments(name).Update(&d)
    if err != nil {
      log.Fatalf("Error updating deployment: %v, %v", err, d)
      return
    }
  }

  ks := v1.Service{}
  err = templateToResource(*s.serviceTemplate, m, &ks)
  if err != nil {
    log.Fatalf("Error templating service: %v", err)
    return
  }
  log.Printf("Creating Service %s", ks.Name)
  _, err = s.KubernetesClientSet.Services(name).Create(&ks)
  if err != nil {
    log.Printf("Error creating service: %v", err)
    log.Printf("*** Assuming error is Service alreayd existing ***")
  }

  s.createOrUpdateIngressFromToken(i, t, path.Backend.ServiceName)

  // This is is the one resource that ends up in the other namespace
  // it is a "new" (shadow) ingress rule to the old service
  m = megaData {
    Name: shadowName,
    Namespace: s.Namespace,
    ServiceName: path.Backend.ServiceName,
    ServicePort: path.Backend.ServicePort.String(),
  }

  ing := v1beta1.Ingress{}
  err = templateToResource(*s.ingressShadowTemplate, m, &ing)
  if err != nil {
    log.Fatalf("Error templating ingress: %v", err)
    return
  }
  log.Printf("Creating Ingress %s in namespace %s", ing.Name, ing.Namespace)
  _, err = s.KubernetesClientSet.Ingresses(ing.Namespace).Create(&ing)
  if err != nil {
    log.Printf("Error creating ingress: %v", err)
    _, err = s.KubernetesClientSet.Ingresses(ing.Namespace).Update(&ing)
    if err != nil {
      log.Fatalf("Error updating ingress: %v, %v", err, ing)
      return
    }
  }

  return
}

func (s Service) createOrUpdateIngressFromToken(i int, t string, b string) (err error) {
  name := getDefaultName(i)
  if b == "" {
    b = name
  }
  m := megaData {
    Name: name,
    Namespace: name,
    ServiceName: b,
    FireflyPath: fmt.Sprintf("/%s", t),
  }

  ing := v1beta1.Ingress{}
  err = templateToResource(*s.ingressTemplate, m, &ing)
  if err != nil {
    log.Fatalf("Error templating ingress: %v", err)
    return
  }
  log.Printf("Creating Ingress %s in namespace %s", ing.Name, ing.Namespace)
  _, err = s.KubernetesClientSet.Ingresses(ing.Namespace).Create(&ing)
  if err != nil {
    log.Printf("Error creating ingress: %v", err)
    _, err = s.KubernetesClientSet.Ingresses(ing.Namespace).Update(&ing)
    if err != nil {
      log.Fatalf("Error updating ingress: %v, %v", err, ing)
      return
    }
  }

  // Another Ingress
  return nil
}

func templateToResource(t template.Template, data interface{}, dest interface{}) (err error) {
  var doc bytes.Buffer
  err = t.Execute(&doc, data)
  if err != nil {
    return
  }
  log.Print(doc.String())

  err = yaml.Unmarshal(doc.Bytes(), dest)
  if err != nil {
    return
  }

  return nil
}

func (s Service) deployScaffolding() (err error) {
  for i := 0; i <= s.MaxDepth; i++ {
    name := getDefaultName(i)
    namePlusOne := getDefaultName(i + 1)
    m := megaData {
      // Namespace Specifc
      Name: name,
      Namespace: name,
      // Deployment Specific
      Selector: name,
      IngressControllerImage: s.IngressControllerImage,
      ContainerName: name,
      WatchNamespace: namePlusOne,
      // Service Specifc
      ServiceName: name,
      TypeString: "type: NodePort",
    }

    ns := v1.Namespace{}
    err = templateToResource(*s.namespaceTemplate, m, &ns)
    if err != nil {
      log.Fatalf("Error templating namespace: %v", err)
      return
    }
    log.Printf("Creating Namespace %s", ns.Name)
    _, err = s.KubernetesClientSet.Namespaces().Create(&ns)
    if err != nil {
      log.Printf("Error creating namespace: %v", err)
      _, err = s.KubernetesClientSet.Namespaces().Update(&ns)
      if err != nil {
        log.Fatalf("Error updating namespace: %v, %v", err, ns)
        return
      }
    }

    d := v1beta1.Deployment{}
    err = templateToResource(*s.deploymentTemplate, m, &d)
    if err != nil {
      log.Fatalf("Error templating deployment: %v", err)
      return
    }
    log.Printf("Creating deployment %s", d.Name)
    _, err = s.KubernetesClientSet.Deployments(ns.Name).Create(&d)
    if err != nil {
      log.Printf("Error creating deployment: %v", err)
      _, err = s.KubernetesClientSet.Deployments(ns.Name).Update(&d)
      if err != nil {
        log.Fatalf("Error updating deployment: %v, %v", err, d)
        return
      }
    }

    ks := v1.Service{}
    err = templateToResource(*s.serviceTemplate, m, &ks)
    if err != nil {
      log.Fatalf("Error templating service: %v", err)
      return
    }
    log.Printf("Creating Service %s", ks.Name)
    _, err = s.KubernetesClientSet.Services(ns.Name).Create(&ks)
    if err != nil {
      log.Printf("Error creating service: %v", err)
      // I guess updating Services isn't as easy as you might think...
      // you need a spec.clusterIP set but we leave ours blank. Hmm...
      log.Printf("*** Assuming error is Service alreayd existing ***")
      // _, err = s.KubernetesClientSet.Services(ns.Name).Update(&ks)
      // if err != nil {
      //   log.Fatalf("Error updating service: %v, %v", err, d)
      //   return
      // }
    }
  }
  return nil
}

// ServiceBuilder is a package export builder object
var ServiceBuilder = builder.Register(serviceBuilder{}, Service{}).(serviceBuilder)
