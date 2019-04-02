/*
 * Tencent is pleased to support the open source community by making 蓝鲸 available.
 * Copyright (C) 2017-2018 THL A29 Limited, a Tencent company. All rights reserved.
 * Licensed under the MIT License (the "License"); you may not use this file except
 * in compliance with the License. You may obtain a copy of the License at
 * http://opensource.org/licenses/MIT
 * Unless required by applicable law or agreed to in writing, software distributed under
 * the License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND,
 * either express or implied. See the License for the specific language governing permissions and
 * limitations under the License.
 */

package service

import (
    "fmt"
	"strconv"
	"strings"

	"configcenter/src/common"
	"configcenter/src/common/blog"
	"configcenter/src/common/condition"
	"configcenter/src/common/mapstr"

	"configcenter/src/common/metadata"
	paraparse "configcenter/src/common/paraparse"
	"configcenter/src/scene_server/topo_server/core/operation"
	"configcenter/src/scene_server/topo_server/core/types"
)

// CreateInst create a new inst
func (s *Service) CreateInst(params types.ContextParams, pathParams, queryParams ParamsGetter, data mapstr.MapStr) (interface{}, error) {

	objID := pathParams("bk_obj_id")
	obj, err := s.Core.ObjectOperation().FindSingleObject(params, objID)
	if nil != err {
		blog.Errorf("failed to search the inst, %s", err.Error())
		return nil, err
	}

	if data.Exists("BatchInfo") {
		/*
		   BatchInfo data format:
		    {
		      "BatchInfo": {
		        "4": { // excel line number
		          "bk_inst_id": 1,
		          "bk_inst_key": "a22",
		          "bk_inst_name": "a11",
		          "bk_version": "121",
		          "import_from": "1"
		        },
		      "input_type": "excel"
		    }
		*/
		batchInfo := new(operation.InstBatchInfo)
		if err := data.MarshalJSONInto(batchInfo); err != nil {
			blog.Errorf("import object[%s] instance batch, but got invalid BatchInfo:[%v] ", objID, batchInfo)
			return nil, params.Err.Error(common.CCErrCommParamsIsInvalid)
		}
		setInst, err := s.Core.InstOperation().CreateInstBatch(params, obj, batchInfo)
		if nil != err {
			blog.Errorf("failed to create new object %s, %s", objID, err.Error())
			return nil, err
		}
		return setInst, nil
	}

	setInst, err := s.Core.InstOperation().CreateInst(params, obj, data)
	if nil != err {
		blog.Errorf("failed to create a new %s, %s", objID, err.Error())
		return nil, err
	}

	return setInst.ToMapStr(), nil
}
func (s *Service) DeleteInsts(params types.ContextParams, pathParams, queryParams ParamsGetter, data mapstr.MapStr) (interface{}, error) {

	obj, err := s.Core.ObjectOperation().FindSingleObject(params, pathParams("bk_obj_id"))
	if nil != err {
		blog.Errorf("[api-inst] failed to find the objects(%s), error info is %s", pathParams("bk_obj_id"), err.Error())
		return nil, err
	}

	deleteCondition := &operation.OpCondition{}
	if err := data.MarshalJSONInto(deleteCondition); nil != err {
		return nil, err
	}

	// auth: deregister resources
	if err := s.AuthManager.DeregisterInstanceByRawID(params.Context, params.Header, deleteCondition.Delete.InstID...); err != nil {
		return nil, fmt.Errorf("deregister instances failed, err: %+v", err)
	}

	return nil, s.Core.InstOperation().DeleteInstByInstID(params, obj, deleteCondition.Delete.InstID, true)
}

// DeleteInst delete the inst
func (s *Service) DeleteInst(params types.ContextParams, pathParams, queryParams ParamsGetter, data mapstr.MapStr) (interface{}, error) {

	if "batch" == pathParams("inst_id") {
		return s.DeleteInsts(params, pathParams, queryParams, data)
	}

	instID, err := strconv.ParseInt(pathParams("inst_id"), 10, 64)
	if nil != err {
		blog.Errorf("[api-inst]failed to parse the inst id, error info is %s", err.Error())
		return nil, params.Err.Errorf(common.CCErrCommParamsNeedInt, "inst id")
	}

	obj, err := s.Core.ObjectOperation().FindSingleObject(params, pathParams("bk_obj_id"))
	if nil != err {
		blog.Errorf("[api-inst] failed to find the objects(%s), error info is %s", pathParams("bk_obj_id"), err.Error())
		return nil, err
	}

	// auth: deregister resources
	if err := s.AuthManager.DeregisterInstanceByRawID(params.Context, params.Header, instID); err != nil {
		return nil, fmt.Errorf("deregister instances failed, err: %+v", err)
	}

	err = s.Core.InstOperation().DeleteInstByInstID(params, obj, []int64{instID}, true)
	return nil, err
}
func (s *Service) UpdateInsts(params types.ContextParams, pathParams, queryParams ParamsGetter, data mapstr.MapStr) (interface{}, error) {

	objID := pathParams("bk_obj_id")

	updateCondition := &operation.OpCondition{}
	if err := data.MarshalJSONInto(updateCondition); nil != err {
		blog.Errorf("[api-inst] failed to parse the input data(%v), error info is %s", data, err.Error())
		return nil, err
	}

	// check inst_id field to be not empty, is dangerous for empty inst_id field, which will update or delete all instance
	for idx, item := range updateCondition.Update {
		if item.InstID == 0 {
			return nil, fmt.Errorf("%d's update item's field `inst_id` emtpy", idx)
		}
	}
	for idx, instID := range updateCondition.Delete.InstID {
		if instID == 0 {
			return nil, fmt.Errorf("%d's delete item's field `inst_id` emtpy", idx)
		}
	}

	obj, err := s.Core.ObjectOperation().FindSingleObject(params, objID)
	if nil != err {
		blog.Errorf("[api-inst] failed to find the objects(%s), error info is %s", pathParams("bk_obj_id"), err.Error())
		return nil, err
	}

	instanceIDs := make([]int64, 0)
	for _, item := range updateCondition.Update {
		instanceIDs = append(instanceIDs, item.InstID)
		cond := condition.CreateCondition()
		cond.Field(obj.GetInstIDFieldName()).Eq(item.InstID)
		err = s.Core.InstOperation().UpdateInst(params, item.InstInfo, obj, cond, item.InstID)
		if nil != err {
			blog.Errorf("[api-inst] failed to update the object(%s) inst (%d),the data (%#v), error info is %s", obj.Object().ObjectID, item.InstID, data, err.Error())
			return nil, err
		}
	}

	// auth: deregister resources
	if err := s.AuthManager.UpdateRegisteredInstanceByID(params.Context, params.Header, instanceIDs...); err != nil {
		return nil, fmt.Errorf("deregister instances failed, err: %+v", err)
	}

	return nil, nil
}

// UpdateInst update the inst
func (s *Service) UpdateInst(params types.ContextParams, pathParams, queryParams ParamsGetter, data mapstr.MapStr) (interface{}, error) {

	if "batch" == pathParams("inst_id") {
		return s.UpdateInsts(params, pathParams, queryParams, data)
	}

	objID := pathParams("bk_obj_id")
	instID, err := strconv.ParseInt(pathParams("inst_id"), 10, 64)
	if nil != err {
		blog.Errorf("[api-inst]failed to parse the inst id, error info is %s", err.Error())
		return nil, params.Err.Errorf(common.CCErrCommParamsNeedInt, "inst id")
	}

	obj, err := s.Core.ObjectOperation().FindSingleObject(params, objID)
	if nil != err {
		blog.Errorf("[api-inst] failed to find the objects(%s), error info is %s", pathParams("bk_obj_id"), err.Error())
		return nil, err
	}

	cond := condition.CreateCondition()
	cond.Field(obj.GetInstIDFieldName()).Eq(instID)
	err = s.Core.InstOperation().UpdateInst(params, data, obj, cond, instID)
	if nil != err {
		blog.Errorf("[api-inst] failed to update the object(%s) inst (%s),the data (%#v), error info is %s", obj.Object().ObjectID, pathParams("inst_id"), data, err.Error())
		return nil, err
	}

	// auth: deregister resources
	if err := s.AuthManager.UpdateRegisteredInstanceByID(params.Context, params.Header, instID); err != nil {
		return nil, fmt.Errorf("deregister instances failed, err: %+v", err)
	}

	return nil, err
}

// SearchInst search the inst
func (s *Service) SearchInsts(params types.ContextParams, pathParams, queryParams ParamsGetter, data mapstr.MapStr) (interface{}, error) {
	objID := pathParams("bk_obj_id")

	obj, err := s.Core.ObjectOperation().FindSingleObject(params, objID)
	if nil != err {
		blog.Errorf("[api-inst] failed to find the objects(%s), error info is %s", pathParams("bk_obj_id"), err.Error())
		return nil, err
	}

	//	if nil != params.MetaData {
	//		data.Set(metadata.BKMetadata, *params.MetaData)
	//	}
	// construct the query inst condition
	queryCond := &paraparse.SearchParams{
		Condition: mapstr.New(),
	}
	if err := data.MarshalJSONInto(queryCond); nil != err {
		blog.Errorf("[api-inst] failed to parse the data and the condition, the input (%#v), error info is %s", data, err.Error())
		return nil, err
	}
	page := metadata.ParsePage(queryCond.Page)
	query := &metadata.QueryInput{}
	query.Condition = queryCond.Condition
	query.Fields = strings.Join(queryCond.Fields, ",")
	query.Limit = page.Limit
	query.Sort = page.Sort
	query.Start = page.Start

	cnt, instItems, err := s.Core.InstOperation().FindInst(params, obj, query, false)
	if nil != err {
		blog.Errorf("[api-inst] failed to find the objects(%s), error info is %s", pathParams("obj_id"), err.Error())
		return nil, err
	}

	result := mapstr.MapStr{}
	result.Set("count", cnt)
	result.Set("info", instItems)
	return result, nil
}

// SearchInstAndAssociationDetail search the inst with association details
func (s *Service) SearchInstAndAssociationDetail(params types.ContextParams, pathParams, queryParams ParamsGetter, data mapstr.MapStr) (interface{}, error) {
	objID := pathParams("bk_obj_id")
	obj, err := s.Core.ObjectOperation().FindSingleObject(params, objID)
	if nil != err {
		blog.Errorf("[api-inst] failed to find the objects(%s), error info is %s", pathParams("bk_obj_id"), err.Error())
		return nil, err
	}

	// construct the query inst condition
	queryCond := &paraparse.SearchParams{
		Condition: mapstr.New(),
	}
	if err := data.MarshalJSONInto(queryCond); nil != err {
		blog.Errorf("[api-inst] failed to parse the data and the condition, the input (%#v), error info is %s", data, err.Error())
		return nil, err
	}
	page := metadata.ParsePage(queryCond.Page)
	query := &metadata.QueryInput{}
	query.Condition = queryCond.Condition
	query.Fields = strings.Join(queryCond.Fields, ",")
	query.Limit = page.Limit
	query.Sort = page.Sort
	query.Start = page.Start

	cnt, instItems, err := s.Core.InstOperation().FindInst(params, obj, query, true)
	if nil != err {
		blog.Errorf("[api-inst] failed to find the objects(%s), error info is %s", pathParams("bk_obj_id"), err.Error())
		return nil, err
	}

	result := mapstr.MapStr{}
	result.Set("count", cnt)
	result.Set("info", instItems)
	return result, nil
}

// SearchInstByObject search the inst of the object
func (s *Service) SearchInstByObject(params types.ContextParams, pathParams, queryParams ParamsGetter, data mapstr.MapStr) (interface{}, error) {

	objID := pathParams("bk_obj_id")
	obj, err := s.Core.ObjectOperation().FindSingleObject(params, objID)
	if nil != err {
		blog.Errorf("[api-inst] failed to find the objects(%s), error info is %s", pathParams("bk_obj_id"), err.Error())
		return nil, err
	}

	queryCond := &paraparse.SearchParams{
		Condition: mapstr.New(),
	}
	if err := data.MarshalJSONInto(queryCond); nil != err {
		blog.Errorf("[api-inst] failed to parse the data and the condition, the input (%#v), error info is %s", data, err.Error())
		return nil, err
	}
	page := metadata.ParsePage(queryCond.Page)
	query := &metadata.QueryInput{}
	query.Condition = queryCond.Condition
	query.Fields = strings.Join(queryCond.Fields, ",")
	query.Limit = page.Limit
	query.Sort = page.Sort
	query.Start = page.Start
	cnt, instItems, err := s.Core.InstOperation().FindInst(params, obj, query, false)
	if nil != err {
		blog.Errorf("[api-inst] failed to find the objects(%s), error info is %s", pathParams("bk_obj_id"), err.Error())
		return nil, err
	}

	result := mapstr.MapStr{}
	result.Set("count", cnt)
	result.Set("info", instItems)
	return result, nil
}

// SearchInstByAssociation search inst by the association inst
func (s *Service) SearchInstByAssociation(params types.ContextParams, pathParams, queryParams ParamsGetter, data mapstr.MapStr) (interface{}, error) {

	objID := pathParams("bk_obj_id")

	obj, err := s.Core.ObjectOperation().FindSingleObject(params, objID)
	if nil != err {
		blog.Errorf("[api-inst] failed to find the objects(%s), error info is %s", pathParams("bk_obj_id"), err.Error())
		return nil, err
	}

	cnt, instItems, err := s.Core.InstOperation().FindInstByAssociationInst(params, obj, data)
	if nil != err {
		blog.Errorf("[api-inst] failed to find the objects(%s), error info is %s", pathParams("bk_obj_id"), err.Error())
		return nil, err
	}

	result := mapstr.MapStr{}
	result.Set("count", cnt)
	result.Set("info", instItems)
	return result, nil
}

// SearchInstByInstID search the inst by inst ID
func (s *Service) SearchInstByInstID(params types.ContextParams, pathParams, queryParams ParamsGetter, data mapstr.MapStr) (interface{}, error) {
	objID := pathParams("obj_id")

	instID, err := strconv.ParseInt(pathParams("inst_id"), 10, 64)
	if nil != err {
		return nil, params.Err.New(common.CCErrTopoInstSelectFailed, err.Error())
	}

	obj, err := s.Core.ObjectOperation().FindSingleObject(params, objID)
	if nil != err {
		blog.Errorf("[api-inst] failed to find the objects(%s), error info is %s", pathParams("bk_obj_id"), err.Error())
		return nil, err
	}

	cond := condition.CreateCondition()
	cond.Field(obj.GetInstIDFieldName()).Eq(instID)
	queryCond := &metadata.QueryInput{}
	queryCond.Condition = cond.ToMapStr()

	cnt, instItems, err := s.Core.InstOperation().FindInst(params, obj, queryCond, false)
	if nil != err {
		blog.Errorf("[api-inst] failed to find the objects(%s), error info is %s", pathParams("bk_obj_id"), err.Error())
		return nil, err
	}

	result := mapstr.MapStr{}
	result.Set("count", cnt)
	result.Set("info", instItems)

	return result, nil
}

// SearchInstChildTopo search the child inst topo for a inst
func (s *Service) SearchInstChildTopo(params types.ContextParams, pathParams, queryParams ParamsGetter, data mapstr.MapStr) (interface{}, error) {
	objID := pathParams("bk_object_id")

	instID, err := strconv.ParseInt(pathParams("inst_id"), 10, 64)
	if nil != err {
		return nil, err
	}

	obj, err := s.Core.ObjectOperation().FindSingleObject(params, objID)
	if nil != err {
		blog.Errorf("[api-inst] failed to find the objects(%s), error info is %s", pathParams("obj_id"), err.Error())
		return nil, err
	}

	query := &metadata.QueryInput{}
	cond := condition.CreateCondition()
	cond.Field(obj.GetInstIDFieldName()).Eq(instID)

	query.Condition = cond.ToMapStr()
	query.Limit = common.BKNoLimit

	_, instItems, err := s.Core.InstOperation().FindInstChildTopo(params, obj, instID, query)
	return instItems, err

}

// SearchInstTopo search the inst topo
func (s *Service) SearchInstTopo(params types.ContextParams, pathParams, queryParams ParamsGetter, data mapstr.MapStr) (interface{}, error) {

	objID := pathParams("bk_obj_id")
	instID, err := strconv.ParseInt(pathParams("inst_id"), 10, 64)
	if nil != err {
		return nil, err
	}

	obj, err := s.Core.ObjectOperation().FindSingleObject(params, objID)
	if nil != err {
		blog.Errorf("[api-inst] failed to find the objects(%s), error info is %s", pathParams("bk_obj_id"), err.Error())
		return nil, err
	}

	query := &metadata.QueryInput{}
	cond := condition.CreateCondition()
	cond.Field(obj.GetInstIDFieldName()).Eq(instID)

	query.Condition = cond.ToMapStr()
	query.Limit = common.BKNoLimit

	_, instItems, err := s.Core.InstOperation().FindInstTopo(params, obj, instID, query)

	return instItems, err
}
