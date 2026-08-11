package httptransport

import (
	"strings"
	"testing"

	authprotocol "aegis/internal/domain/authprotocol"
	"aegis/internal/service"
)

// 生成式客户端（Android / Kotlin / Swift / Dart）整个是从这份规范产出的，
// 因此这几条一旦破掉，产出的客户端就直接不可用：
//
//	缺 operationId 短名 → 方法名变成 post__api__v1__apps__by_appkey__auth__login
//	缺 requestBody     → 写接口生成出来是个没有参数的空方法
//	缺 security        → 生成的客户端不带 Authorization 头，所有鉴权接口 401
func TestGatewayOpenAPICoversEveryCatalogOperation(t *testing.T) {
	spec, err := BuildOpenAPISpec(newTestRouter(t), DefaultDocsOptions())
	if err != nil {
		t.Fatalf("BuildOpenAPISpec: %v", err)
	}

	models := gatewayRequestModels()
	for _, catalogOp := range service.GatewayOperations() {
		path := gatewayOpenAPIPrefix + catalogOp.Path
		item := spec.Paths.Find(path)
		if item == nil {
			t.Errorf("规范里没有 %s", path)
			continue
		}
		if !gatewayDocMethods[catalogOp.Method] {
			t.Errorf("%s %s 用了规范生成未覆盖的方法", catalogOp.Method, path)
			continue
		}
		operation := item.GetOperation(catalogOp.Method)
		if operation == nil {
			t.Errorf("规范里没有 %s %s", catalogOp.Method, path)
			continue
		}

		wantID := gatewayOperationID(catalogOp.Key)
		if operation.OperationID != wantID {
			t.Errorf("%s %s 的 operationId 应为 %q，实际 %q",
				catalogOp.Method, path, wantID, operation.OperationID)
		}
		if catalogOp.Auth {
			if operation.Security == nil || len(*operation.Security) == 0 {
				t.Errorf("%s %s 需要 Bearer，但规范里没有 security", catalogOp.Method, path)
			}
		}
		if catalogOp.Upload {
			if operation.RequestBody == nil || operation.RequestBody.Value == nil ||
				operation.RequestBody.Value.Content.Get("multipart/form-data") == nil {
				t.Errorf("%s %s 是上传接口，规范里必须有 multipart/form-data 请求体",
					catalogOp.Method, path)
			}
			continue
		}
		if _, declared := models[catalogOp.Key]; !declared {
			continue
		}
		// 声明了模型就必须落到规范上：GET 摊成查询参数，其余摊成请求体。
		if authprotocol.BodylessMethod(catalogOp.Method) {
			if len(operation.Parameters) == 0 {
				t.Errorf("%s %s 声明了查询模型，规范里却没有任何参数", catalogOp.Method, path)
			}
			continue
		}
		if operation.RequestBody == nil || operation.RequestBody.Value == nil ||
			operation.RequestBody.Value.Content.Get("application/json") == nil {
			t.Errorf("%s %s 声明了请求模型，规范里却没有 JSON 请求体", catalogOp.Method, path)
		}
	}
}

// operationId 全局唯一是 OpenAPI 的硬要求，重复会让 openapi-generator
// 悄悄丢掉后面那条 —— 表现是「某个接口在生成的客户端里根本不存在」。
func TestGatewayOperationIDsAreUniqueAcrossSpec(t *testing.T) {
	spec, err := BuildOpenAPISpec(newTestRouter(t), DefaultDocsOptions())
	if err != nil {
		t.Fatalf("BuildOpenAPISpec: %v", err)
	}
	seen := map[string]string{}
	for path, item := range spec.Paths.Map() {
		for method, operation := range item.Operations() {
			if operation.OperationID == "" {
				t.Errorf("%s %s 缺少 operationId", method, path)
				continue
			}
			if previous, exists := seen[operation.OperationID]; exists {
				t.Errorf("operationId %q 重复：%s 与 %s %s",
					operation.OperationID, previous, method, path)
			}
			seen[operation.OperationID] = method + " " + path
		}
	}
}

// 网关接口全部归到同一个 tag 下：生成式客户端按 tag 分文件，
// 散落到十几个 tag 里会让接入方拿到一堆互不相干的 Api 类。
func TestGatewayOperationsShareOneTag(t *testing.T) {
	spec, err := BuildOpenAPISpec(newTestRouter(t), DefaultDocsOptions())
	if err != nil {
		t.Fatalf("BuildOpenAPISpec: %v", err)
	}
	for path, item := range spec.Paths.Map() {
		if !strings.HasPrefix(path, gatewayOpenAPIPrefix) {
			continue
		}
		for method, operation := range item.Operations() {
			if len(operation.Tags) != 1 || operation.Tags[0] != gatewayDocTag {
				t.Errorf("%s %s 的 tag 应为 [%s]，实际 %v",
					method, path, gatewayDocTag, operation.Tags)
			}
		}
	}
}
