package valueobject

// User 用户值对象（不可变）
type User struct {
	id       string
	username string
	userType string
	metadata map[string]string
}

// NewUser 创建用户值对象
func NewUser(id, username, userType string) User {
	return User{
		id:       id,
		username: username,
		userType: userType,
		metadata: make(map[string]string),
	}
}

// NewUserWithMetadata 创建带元数据的用户值对象
func NewUserWithMetadata(id, username, userType string, metadata map[string]string) User {
	// 值对象不可变，创建副本
	meta := make(map[string]string)
	for k, v := range metadata {
		meta[k] = v
	}

	return User{
		id:       id,
		username: username,
		userType: userType,
		metadata: meta,
	}
}

// ID 返回用户ID
func (u User) ID() string {
	return u.id
}

// Username 返回用户名
func (u User) Username() string {
	return u.username
}

// Type 返回用户类型
func (u User) Type() string {
	return u.userType
}

// Metadata 返回元数据（副本）
func (u User) Metadata() map[string]string {
	meta := make(map[string]string)
	for k, v := range u.metadata {
		meta[k] = v
	}
	return meta
}

// GetMetadata 获取元数据值
func (u User) GetMetadata(key string) (string, bool) {
	val, ok := u.metadata[key]
	return val, ok
}

// IsAnonymous 判断是否匿名用户
func (u User) IsAnonymous() bool {
	return u.userType == "anonymous"
}

// Equals 值对象相等性比较
func (u User) Equals(other User) bool {
	if u.id != other.id || u.username != other.username || u.userType != other.userType {
		return false
	}

	if len(u.metadata) != len(other.metadata) {
		return false
	}

	for k, v := range u.metadata {
		if otherV, ok := other.metadata[k]; !ok || v != otherV {
			return false
		}
	}

	return true
}
