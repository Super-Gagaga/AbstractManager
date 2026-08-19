package unit

import (
	"testing"

	"github.com/Super-Gagaga/AbstractManager/tests/testutil"
	"github.com/Super-Gagaga/AbstractManager/util/cache_key_builder"

	"github.com/stretchr/testify/assert"
)

// =============================================================================
// UT-201 ~ UT-205: TemplateKeyBuilder
// =============================================================================

func TestTemplateKeyBuilder_Normal(t *testing.T) {
	builder := cache_key_builder.NewTemplateKeyBuilder[testutil.TestUser]("user:{id}")
	user := testutil.TestUser{ID: 5, Username: "alice"}

	key := builder.BuildKey(&user)
	assert.Equal(t, "user:5", key)
}

func TestTemplateKeyBuilder_MultiField(t *testing.T) {
	builder := cache_key_builder.NewTemplateKeyBuilder[testutil.Product]("product:{id}:{category}")
	product := testutil.Product{ID: 1, Category: "electronics", Name: "Laptop"}

	key := builder.BuildKey(&product)
	assert.Equal(t, "product:1:electronics", key)
}

func TestTemplateKeyBuilder_Nested(t *testing.T) {
	builder := cache_key_builder.NewTemplateKeyBuilder[testutil.NestedContainer]("cache:{user.id}")
	data := testutil.NestedContainer{}
	data.User.ID = 42
	data.User.Name = "bob"

	key := builder.BuildKey(&data)
	assert.Equal(t, "cache:42", key)
}

func TestTemplateKeyBuilder_NilData(t *testing.T) {
	builder := cache_key_builder.NewTemplateKeyBuilder[testutil.TestUser]("user:{id}")

	key := builder.BuildKey(nil)
	// When data is nil, returns the template as-is
	assert.Equal(t, "user:{id}", key)
}

func TestTemplateKeyBuilder_JsonTag(t *testing.T) {
	// Json tag "json:username" should match by field name first, then by json tag
	builder := cache_key_builder.NewTemplateKeyBuilder[testutil.TestUser]("user:{username}")
	user := testutil.TestUser{ID: 1, Username: "charlie"}

	key := builder.BuildKey(&user)
	assert.Equal(t, "user:charlie", key)
}

func TestTemplateKeyBuilder_CaseInsensitive(t *testing.T) {
	// Template uses lowercase "id" but struct field is "ID"
	builder := cache_key_builder.NewTemplateKeyBuilder[testutil.TestUser]("user:{id}")
	user := testutil.TestUser{ID: 99}

	key := builder.BuildKey(&user)
	assert.Equal(t, "user:99", key)
}

func TestTemplateKeyBuilder_UnknownField(t *testing.T) {
	builder := cache_key_builder.NewTemplateKeyBuilder[testutil.TestUser]("user:{nonexistent}")
	user := testutil.TestUser{ID: 1}

	key := builder.BuildKey(&user)
	// Unknown field → placeholder stays in template
	assert.Contains(t, key, "{nonexistent}")
}

func TestTemplateKeyBuilder_PointerData(t *testing.T) {
	builder := cache_key_builder.NewTemplateKeyBuilder[testutil.TestUser]("user:{id}")
	user := &testutil.TestUser{ID: 77}

	key := builder.BuildKey(user)
	assert.Equal(t, "user:77", key)
}

// =============================================================================
// UT-206: PrefixKeyBuilder
// =============================================================================

func TestPrefixKeyBuilder(t *testing.T) {
	builder := cache_key_builder.NewPrefixKeyBuilder[testutil.TestUser]("user", func(u *testutil.TestUser) interface{} {
		return u.ID
	})

	user := testutil.TestUser{ID: 10, Username: "dave"}
	key := builder.BuildKey(&user)
	assert.Equal(t, "user:10", key)
}

func TestPrefixKeyBuilder_NilData(t *testing.T) {
	builder := cache_key_builder.NewPrefixKeyBuilder[testutil.TestUser]("user", func(u *testutil.TestUser) interface{} {
		return u.ID
	})

	key := builder.BuildKey(nil)
	assert.Equal(t, "user", key)
}

// =============================================================================
// FuncKeyBuilder
// =============================================================================

func TestFuncKeyBuilder(t *testing.T) {
	builder := cache_key_builder.NewFuncKeyBuilder[testutil.TestUser](func(u *testutil.TestUser) string {
		return "custom:" + u.Username
	})

	user := testutil.TestUser{ID: 1, Username: "eve"}
	key := builder.BuildKey(&user)
	assert.Equal(t, "custom:eve", key)
}

// =============================================================================
// QuickBuildKey
// =============================================================================

func TestQuickBuildKey(t *testing.T) {
	user := testutil.TestUser{ID: 42, Username: "frank"}

	key := cache_key_builder.QuickBuildKey[testutil.TestUser]("cache:user:{id}", &user)
	assert.Equal(t, "cache:user:42", key)
}

// =============================================================================
// BuildKeyFunc adapter
// =============================================================================

func TestBuildKeyFunc(t *testing.T) {
	templateBuilder := cache_key_builder.NewTemplateKeyBuilder[testutil.TestUser]("prefix:{id}")
	fn := cache_key_builder.BuildKeyFunc(templateBuilder)

	user := testutil.TestUser{ID: 7}
	key := fn(&user)
	assert.Equal(t, "prefix:7", key)
}

// =============================================================================
// DefaultKeyBuilder
// =============================================================================

func TestDefaultKeyBuilder(t *testing.T) {
	builder := cache_key_builder.DefaultKeyBuilder[testutil.TestUser]("user:{id}")
	user := testutil.TestUser{ID: 15}

	key := builder.BuildKey(&user)
	assert.Equal(t, "user:15", key)
}
