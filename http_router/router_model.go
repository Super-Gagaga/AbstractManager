package http_router

import (
	serviceManager "github.com/Super-Gagaga/AbstractManager/service"
)

// HTTPRouterManager 封装了 ServiceManager 并提供 HTTP 路由注册功能
type HTTPRouterManager[T any] struct {
	// 1. 使用指针嵌入 (*)，避免拷贝
	// 2. 必须显式传递泛型参数 [T]
	*serviceManager.ServiceManager[T]
}

// NewHTTPRouterManager 构造函数 (推荐方式：依赖注入)
// 接收一个已经初始化好的 ServiceManager
func NewHTTPRouterManager[T any](svc *serviceManager.ServiceManager[T]) *HTTPRouterManager[T] {
	return &HTTPRouterManager[T]{
		ServiceManager: svc,
	}
}

// NewHTTPRouterManagerFromModel 构造函数 (兼容你原来的方式)
// 接收模型实例，在内部创建新的 ServiceManager
// 注意：如果在多个地方使用，可能会导致创建多个 DB 连接池，请谨慎使用
func NewHTTPRouterManagerFromModel[T any](model T) *HTTPRouterManager[T] {
	return &HTTPRouterManager[T]{
		ServiceManager: serviceManager.NewServiceManager(model),
	}
}
