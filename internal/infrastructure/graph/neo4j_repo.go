package graph

import (
	"context"
	"fmt"
	"log"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

// 给每个节点 (Node) 和关系 (Relationship) 都打上 _owner_id 属性
type Neo4jRepo struct {
	driver neo4j.DriverWithContext
}

func NewNeo4jRepo(uri, user, password string) (*Neo4jRepo, error) {
	driver, err := neo4j.NewDriverWithContext(uri, neo4j.BasicAuth(user, password, ""))
	if err != nil {
		return nil, err
	}
	if err = driver.VerifyConnectivity(context.Background()); err != nil {
		return nil, err
	}
	return &Neo4jRepo{driver: driver}, nil
}

type Entity struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

type Relation struct {
	Source Entity `json:"source"`
	Target Entity `json:"target"`
	Type   string `json:"type"`
}

// SaveKnowledgeGraph 将提取出的三元组存入 Neo4j
func (r *Neo4jRepo) SaveKnowledgeGraph(ctx context.Context, entities []Entity, relations []Relation, ownerID string) error {
	session := r.driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeWrite})
	defer session.Close(ctx)

	_, err := session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
		// 存节点 Merge去重
		for _, entity := range entities {
			query := fmt.Sprintf("MERGE (n:%s {name: $name,_owner_id: $owner})", entity.Type)
			_, err := tx.Run(ctx, query, map[string]any{"name": entity.Name, "owner": ownerID})
			if err != nil {
				return nil, fmt.Errorf("failed to create entity %s: %v", entity.Name, err)
			}
		}

		for _, relation := range relations {
			// 允许用户创建连接到system节点的关系
			// 源节点和目标节点可以是用户的或system的，但关系的owner是创建者
			query := fmt.Sprintf(`
MATCH (s:%s {name: $sName})
WHERE s._owner_id = $owner OR s._owner_id = 'system'
MATCH (t:%s {name: $tName})
WHERE t._owner_id = $owner OR t._owner_id = 'system'
MERGE (s)-[:%s { _owner_id:$owner }]->(t)
`, relation.Source.Type, relation.Target.Type, relation.Type)
			_, err := tx.Run(ctx, query, map[string]any{
				"sName": relation.Source.Name,
				"tName": relation.Target.Name,
				"owner": ownerID,
			})
			if err != nil {
				return nil, fmt.Errorf("failed to create relation %s-%s: %v", relation.Source.Name, relation.Target.Name, err)
			}
		}
		return nil, nil
	})
	if err == nil {
		log.Printf(" Graph DB updated: %d entities, %d relations", len(entities), len(relations))
	}
	return err
}

// GetRelatedKnowledge 查询指定实体的一跳邻居信息
// 返回格式示例: "Ankit Goyal --[AUTHORED]--> VLA-0"
func (r *Neo4jRepo) GetRelatedKnowledge(ctx context.Context, keyword string, userID string) ([]string, error) {
	session := r.driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeRead})
	defer session.Close(ctx)

	result, err := session.ExecuteRead(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
		// Cypher 查询：
		// 查找名字包含 keyword 的节点 (n)，并找出它所有的关系 (r) 和邻居 (m)
		// 确保节点n、关系r和邻居节点m都属于当前用户或system
		// LIMIT 20 防止返回太多炸掉 Token
		query := `
			MATCH (n)-[r]-(m)
			WHERE toLower(n.name) CONTAINS toLower($keyword)
			AND (n._owner_id = $userID OR n._owner_id = 'system')
        	AND (r._owner_id = $userID OR r._owner_id = 'system')
			AND (m._owner_id = $userID OR m._owner_id = 'system')
			RETURN n.name, type(r), m.name
			LIMIT 20
		`
		records, err := tx.Run(ctx, query, map[string]any{"keyword": keyword, "userID": userID})
		if err != nil {
			return nil, err
		}

		var knowledge []string
		for records.Next(ctx) {
			record := records.Record()
			nName, _ := record.Get("n.name")
			rType, _ := record.Get("type(r)")
			mName, _ := record.Get("m.name")
			line := fmt.Sprintf("%s --[%s]--> %s", nName.(string), rType.(string), mName.(string))
			knowledge = append(knowledge, line)
		}
		return knowledge, nil
	})
	if err != nil {
		return nil, err
	}
	return result.([]string), nil
}

func (r *Neo4jRepo) Close(ctx context.Context) {
	r.driver.Close(ctx)
}
