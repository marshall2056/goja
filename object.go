package goja

import "reflect"

const (
	classObject   = "Object"
	classArray    = "Array"
	classFunction = "Function"
	classNumber   = "Number"
	classString   = "String"
	classBoolean  = "Boolean"
	classError    = "Error"
	classRegExp   = "RegExp"
	classDate     = "Date"
)

type Object struct {
	runtime *Runtime
	self    objectImpl
}

type iterNextFunc func() (propIterItem, iterNextFunc)

type objectImpl interface {
	sortable
	className() string
	get(Value) Value
	getProp(Value) Value
	getPropStr(string) Value
	getStr(string) Value
	getOwnProp(string) Value
	put(Value, Value, bool)
	putStr(string, Value, bool)
	hasProperty(Value) bool
	hasPropertyStr(string) bool
	hasOwnProperty(Value) bool
	hasOwnPropertyStr(string) bool
	_putProp(name string, value Value, writable, enumerable, configurable bool) Value
	defineOwnProperty(name Value, descr objectImpl, throw bool) bool
	toPrimitiveNumber() Value
	toPrimitiveString() Value
	toPrimitive() Value
	assertCallable() (call func(FunctionCall) Value, ok bool)
	// defineOwnProperty(Value, property, bool) bool
	deleteStr(name string, throw bool) bool
	delete(name Value, throw bool) bool
	proto() *Object
	hasInstance(v Value) bool
	isExtensible() bool
	preventExtensions()
	enumerate(all, recusrive bool) iterNextFunc
	_enumerate(recursive bool) iterNextFunc
	export() interface{}
	exportType() reflect.Type
	equal(objectImpl) bool

	// clone(*_object, *_object, *_clone) *_object
	// marshalJSON() json.Marshaler

}

type baseObject struct {
	class      string
	val        *Object
	prototype  *Object
	extensible bool

	values    map[string]Value
	propNames []string
}

type funcObject struct {
	baseObject

	nameProp, lenProp valueProperty

	stash *stash
	prg   *Program
	src   string
}

type primitiveValueObject struct {
	baseObject
	pValue Value
}

func (o *primitiveValueObject) export() interface{} {
	return o.pValue.Export()
}

func (o *primitiveValueObject) exportType() reflect.Type {
	return o.pValue.ExportType()
}

type FunctionCall struct {
	This      Value
	Arguments []Value
}

func (f FunctionCall) Argument(idx int) Value {
	if idx < len(f.Arguments) {
		return f.Arguments[idx]
	}
	return _undefined
}

type nativeFuncObject struct {
	baseObject
	nameProp, lenProp valueProperty
	f                 func(FunctionCall) Value
	construct         func(args []Value) *Object
}

func (f *nativeFuncObject) export() interface{} {
	return f.f
}

func (f *nativeFuncObject) exportType() reflect.Type {
	return reflect.TypeOf(f.f)
}

type boundFuncObject struct {
	nativeFuncObject
}

func (o *baseObject) init() {
	o.values = make(map[string]Value)
}

func (o *baseObject) className() string {
	return o.class
}

func (o *baseObject) getPropStr(name string) Value {
	if val := o.getOwnProp(name); val != nil {
		return val
	}
	if o.prototype != nil {
		return o.prototype.self.getPropStr(name)
	}
	return nil
}

func (o *baseObject) getProp(n Value) Value {
	return o.val.self.getPropStr(n.String())
}

func (o *baseObject) hasProperty(n Value) bool {
	return o.val.self.getProp(n) != nil
}

func (o *baseObject) hasPropertyStr(name string) bool {
	return o.val.self.getPropStr(name) != nil
}

func (o *baseObject) _getStr(name string) Value {
	p := o.getOwnProp(name)

	if p == nil && o.prototype != nil {
		p = o.prototype.self.getPropStr(name)
	}

	if p, ok := p.(*valueProperty); ok {
		return p.get(o.val)
	}

	return p
}

func (o *baseObject) getStr(name string) Value {
	p := o.val.self.getPropStr(name)
	if p, ok := p.(*valueProperty); ok {
		return p.get(o.val)
	}

	return p
}

func (o *baseObject) get(n Value) Value {
	return o.getStr(n.String())
}

func (o *baseObject) checkDeleteProp(name string, prop *valueProperty, throw bool) bool {
	if !prop.configurable {
		o.val.runtime.typeErrorResult(throw, "Cannot delete property '%s' of %s", name, o.val.ToString())
		return false
	}
	return true
}

func (o *baseObject) checkDelete(name string, val Value, throw bool) bool {
	if val, ok := val.(*valueProperty); ok {
		return o.checkDeleteProp(name, val, throw)
	}
	return true
}

func (o *baseObject) _delete(name string) {
	delete(o.values, name)
	for i, n := range o.propNames {
		if n == name {
			copy(o.propNames[i:], o.propNames[i+1:])
			o.propNames = o.propNames[:len(o.propNames)-1]
			break
		}
	}
}

func (o *baseObject) deleteStr(name string, throw bool) bool {
	if val, exists := o.values[name]; exists {
		if !o.checkDelete(name, val, throw) {
			return false
		}
		o._delete(name)
		return true
	}
	return true
}

func (o *baseObject) delete(n Value, throw bool) bool {
	return o.deleteStr(n.String(), throw)
}

func (o *baseObject) put(n Value, val Value, throw bool) {
	o.putStr(n.String(), val, throw)
}

func (o *baseObject) getOwnProp(name string) Value {
	v := o.values[name]
	if v == nil && name == "__proto" {
		return o.prototype
	}
	return v
}

func (o *baseObject) putStr(name string, val Value, throw bool) {
	if v, exists := o.values[name]; exists {
		if prop, ok := v.(*valueProperty); ok {
			if !prop.isWritable() {
				o.val.runtime.typeErrorResult(throw, "Cannot assign to read only property '%s'", name)
				return
			}
			prop.set(o.val, val)
			return
		}
		o.values[name] = val
		return
	}

	if name == "__proto__" {
		if !o.extensible {
			o.val.runtime.typeErrorResult(throw, "%s is not extensible", o.val)
			return
		}
		if val == _undefined || val == _null {
			o.prototype = nil
			return
		} else {
			if val, ok := val.(*Object); ok {
				o.prototype = val
			}
		}
		return
	}

	var pprop Value
	if proto := o.prototype; proto != nil {
		pprop = proto.self.getPropStr(name)
	}

	if pprop != nil {
		if prop, ok := pprop.(*valueProperty); ok {
			if !prop.isWritable() {
				o.val.runtime.typeErrorResult(throw)
				return
			}
			if prop.accessor {
				prop.set(o.val, val)
				return
			}
		}
	} else {
		if !o.extensible {
			o.val.runtime.typeErrorResult(throw)
			return
		}
	}

	o.values[name] = val
	o.propNames = append(o.propNames, name)
}

func (o *baseObject) hasOwnProperty(n Value) bool {
	v := o.values[n.String()]
	return v != nil
}

func (o *baseObject) hasOwnPropertyStr(name string) bool {
	v := o.values[name]
	return v != nil
}

func (o *baseObject) _defineOwnProperty(name Value, existingValue Value, descr objectImpl, throw bool) (val Value, ok bool) {
	var hasWritable, hasEnumerable, hasConfigurable bool
	var writable, enumerable, configurable bool

	value := descr.getStr("value")

	if p := descr.getStr("writable"); p != nil {
		hasWritable = true
		writable = p.ToBoolean()
	}
	if p := descr.getStr("enumerable"); p != nil {
		hasEnumerable = true
		enumerable = p.ToBoolean()
	}
	if p := descr.getStr("configurable"); p != nil {
		hasConfigurable = true
		configurable = p.ToBoolean()
	}

	getter := descr.getStr("get")
	setter := descr.getStr("set")

	if (getter != nil || setter != nil) && (value != nil || hasWritable) {
		o.val.runtime.typeErrorResult(throw, "Invalid property descriptor. Cannot both specify accessors and a value or writable attribute")
		return nil, false
	}

	getterObj, _ := getter.(*Object)
	setterObj, _ := setter.(*Object)

	var existing *valueProperty

	if existingValue == nil {
		if !o.extensible {
			o.val.runtime.typeErrorResult(throw)
			return nil, false
		}
		existing = &valueProperty{}
	} else {
		if existing, ok = existingValue.(*valueProperty); !ok {
			existing = &valueProperty{
				writable:     true,
				enumerable:   true,
				configurable: true,
				value:        existingValue,
			}
		}

		if !existing.configurable {
			if configurable {
				goto Reject
			}
			if hasEnumerable && enumerable != existing.enumerable {
				goto Reject
			}
		}
		if existing.accessor && value != nil || !existing.accessor && (getterObj != nil || setterObj != nil) {
			if !existing.configurable {
				goto Reject
			}
		} else if !existing.accessor {
			if !existing.configurable {
				if !existing.writable {
					if writable {
						goto Reject
					}
					if value != nil && !value.SameAs(existing.value) {
						goto Reject
					}
				}
			}
		} else {
			if !existing.configurable {
				if getter != nil && existing.getterFunc != getterObj || setter != nil && existing.setterFunc != setterObj {
					goto Reject
				}
			}
		}
	}

	if writable && enumerable && configurable && value != nil {
		return value, true
	}

	if hasWritable {
		existing.writable = writable
	}
	if hasEnumerable {
		existing.enumerable = enumerable
	}
	if hasConfigurable {
		existing.configurable = configurable
	}

	if value != nil {
		existing.value = value
		existing.getterFunc = nil
		existing.setterFunc = nil
	}

	if value != nil || hasWritable {
		existing.accessor = false
	}

	if getter != nil {
		existing.getterFunc = propGetter(o.val, getter, o.val.runtime)
		existing.value = nil
		existing.accessor = true
	}

	if setter != nil {
		existing.setterFunc = propSetter(o.val, setter, o.val.runtime)
		existing.value = nil
		existing.accessor = true
	}

	if !existing.accessor && existing.value == nil {
		existing.value = _undefined
	}

	return existing, true

Reject:
	o.val.runtime.typeErrorResult(throw, "Cannot redefine property: %s", name.ToString())
	return nil, false
}

func (o *baseObject) defineOwnProperty(n Value, descr objectImpl, throw bool) bool {
	name := n.String()
	val := o.values[name]
	if v, ok := o._defineOwnProperty(n, val, descr, throw); ok {
		o.values[name] = v
		if val == nil {
			o.propNames = append(o.propNames, name)
		}
		return true
	}
	return false
}

func (o *baseObject) _put(name string, v Value) {
	if _, exists := o.values[name]; !exists {
		o.propNames = append(o.propNames, name)
	}

	o.values[name] = v
}

func (o *baseObject) _putProp(name string, value Value, writable, enumerable, configurable bool) Value {
	if writable && enumerable && configurable {
		o._put(name, value)
		return value
	} else {
		p := &valueProperty{
			value:        value,
			writable:     writable,
			enumerable:   enumerable,
			configurable: configurable,
		}
		o._put(name, p)
		return p
	}
}

func (o *baseObject) tryPrimitive(methodName string) Value {
	if method, ok := o.getStr(methodName).(*Object); ok {
		if call, ok := method.self.assertCallable(); ok {
			v := call(FunctionCall{
				This: o.val,
			})
			if _, fail := v.(*Object); !fail {
				return v
			}
		}
	}
	return nil
}

func (o *baseObject) toPrimitiveNumber() Value {
	if v := o.tryPrimitive("valueOf"); v != nil {
		return v
	}

	if v := o.tryPrimitive("toString"); v != nil {
		return v
	}

	o.val.runtime.typeErrorResult(true, "Could not convert %v to primitive", o)
	return nil
}

func (o *baseObject) toPrimitiveString() Value {
	if v := o.tryPrimitive("toString"); v != nil {
		return v
	}

	if v := o.tryPrimitive("valueOf"); v != nil {
		return v
	}

	o.val.runtime.typeErrorResult(true, "Could not convert %v to primitive", o)
	return nil
}

func (o *baseObject) toPrimitive() Value {
	return o.toPrimitiveNumber()
}

func (o *baseObject) assertCallable() (func(FunctionCall) Value, bool) {
	return nil, false
}

func (o *baseObject) proto() *Object {
	return o.prototype
}

func (o *baseObject) isExtensible() bool {
	return o.extensible
}

func (o *baseObject) preventExtensions() {
	o.extensible = false
}

func (o *baseObject) sortLen() int64 {
	return toLength(o.val.self.getStr("length"))
}

func (o *baseObject) sortGet(i int64) Value {
	return o.val.self.get(intToValue(i))
}

func (o *baseObject) swap(i, j int64) {
	ii := intToValue(i)
	jj := intToValue(j)

	x := o.val.self.get(ii)
	y := o.val.self.get(jj)

	o.val.self.put(ii, y, false)
	o.val.self.put(jj, x, false)
}

func (o *baseObject) export() interface{} {
	m := make(map[string]interface{})

	for item, f := o.enumerate(false, false)(); f != nil; item, f = f() {
		v := item.value
		if v == nil {
			v = o.getStr(item.name)
		}
		if v != nil {
			m[item.name] = v.Export()
		} else {
			m[item.name] = nil
		}
	}
	return m
}

func (o *baseObject) exportType() reflect.Type {
	return reflectTypeMap
}

type enumerableFlag int

const (
	_ENUM_UNKNOWN enumerableFlag = iota
	_ENUM_FALSE
	_ENUM_TRUE
)

type propIterItem struct {
	name       string
	value      Value // set only when enumerable == _ENUM_UNKNOWN
	enumerable enumerableFlag
}

type objectPropIter struct {
	o         *baseObject
	propNames []string
	recursive bool
	idx       int
}

type propFilterIter struct {
	wrapped iterNextFunc
	all     bool
	seen    map[string]bool
}

func (i *propFilterIter) next() (propIterItem, iterNextFunc) {
	for {
		var item propIterItem
		item, i.wrapped = i.wrapped()
		if i.wrapped == nil {
			return propIterItem{}, nil
		}

		if !i.seen[item.name] {
			i.seen[item.name] = true
			if !i.all {
				if item.enumerable == _ENUM_FALSE {
					continue
				}
				if item.enumerable == _ENUM_UNKNOWN {
					if prop, ok := item.value.(*valueProperty); ok {
						if !prop.enumerable {
							continue
						}
					}
				}
			}
			return item, i.next
		}
	}
}

func (i *objectPropIter) next() (propIterItem, iterNextFunc) {
	for i.idx < len(i.propNames) {
		name := i.propNames[i.idx]
		i.idx++
		prop := i.o.values[name]
		if prop != nil {
			return propIterItem{name: name, value: prop}, i.next
		}
	}

	if i.recursive && i.o.prototype != nil {
		return i.o.prototype.self._enumerate(i.recursive)()
	}
	return propIterItem{}, nil
}

func (o *baseObject) _enumerate(recusrive bool) iterNextFunc {
	propNames := make([]string, len(o.propNames))
	copy(propNames, o.propNames)
	return (&objectPropIter{
		o:         o,
		propNames: propNames,
		recursive: recusrive,
	}).next
}

func (o *baseObject) enumerate(all, recursive bool) iterNextFunc {
	return (&propFilterIter{
		wrapped: o._enumerate(recursive),
		all:     all,
		seen:    make(map[string]bool),
	}).next
}

func (o *baseObject) equal(other objectImpl) bool {
	// Rely on parent reference comparison
	return false
}

func (f *funcObject) getPropStr(name string) Value {
	switch name {
	case "prototype":
		if _, exists := f.values["prototype"]; !exists {
			return f.addPrototype()
		}
	}

	return f.baseObject.getPropStr(name)
}

func (f *funcObject) addPrototype() Value {
	proto := f.val.runtime.NewObject()
	proto.self._putProp("constructor", f.val, true, false, true)
	return f._putProp("prototype", proto, true, false, false)
}

func (f *funcObject) getProp(n Value) Value {
	return f.getPropStr(n.String())
}

func (f *funcObject) hasOwnProperty(n Value) bool {
	if r := f.baseObject.hasOwnProperty(n); r {
		return true
	}

	name := n.String()
	if name == "prototype" {
		return true
	}
	return false
}

func (f *funcObject) hasOwnPropertyStr(name string) bool {
	if r := f.baseObject.hasOwnPropertyStr(name); r {
		return true
	}

	if name == "prototype" {
		return true
	}
	return false
}

func (f *funcObject) construct(args []Value) *Object {
	proto := f.getStr("prototype")
	var protoObj *Object
	if p, ok := proto.(*Object); ok {
		protoObj = p
	} else {
		protoObj = f.val.runtime.global.ObjectPrototype
	}
	obj := f.val.runtime.newBaseObject(protoObj, classObject).val
	ret := f.Call(FunctionCall{
		This:      obj,
		Arguments: args,
	})

	if ret, ok := ret.(*Object); ok {
		return ret
	}
	return obj
}

func (f *funcObject) Call(call FunctionCall) Value {
	vm := f.val.runtime.vm
	pc := vm.pc
	vm.push(f.val)
	vm.push(call.This)
	for _, arg := range call.Arguments {
		vm.push(arg)
	}
	vm.pc = -1
	vm.pushCtx()
	vm.args = len(call.Arguments)
	vm.prg = f.prg
	vm.stash = f.stash
	vm.pc = 0
	vm.run()
	vm.pc = pc
	vm.halt = false
	return vm.pop()
}

func (f *funcObject) export() interface{} {
	return f.Call
}

func (f *funcObject) exportType() reflect.Type {
	return reflect.TypeOf(f.Call)
}

func (f *funcObject) assertCallable() (func(FunctionCall) Value, bool) {
	return f.Call, true
}

func (f *funcObject) init(name string, length int) {
	f.baseObject.init()

	f.nameProp.configurable = true
	f.nameProp.value = newStringValue(name)
	f.values["name"] = &f.nameProp

	f.lenProp.configurable = true
	f.lenProp.value = valueInt(length)
	f.values["length"] = &f.lenProp
}

func (o *baseObject) hasInstance(v Value) bool {
	o.val.runtime.typeErrorResult(true, "Expecting a function in instanceof check, but got %s", o.val.ToString())
	panic("Unreachable")
}

func (f *funcObject) hasInstance(v Value) bool {
	return f._hasInstance(v)
}

func (f *nativeFuncObject) hasInstance(v Value) bool {
	return f._hasInstance(v)
}

func (f *baseObject) _hasInstance(v Value) bool {
	if v, ok := v.(*Object); ok {
		o := f.val.self.getStr("prototype")
		if o1, ok := o.(*Object); ok {
			for {
				v = v.self.proto()
				if v == nil {
					return false
				}
				if o1 == v {
					return true
				}
			}
		} else {
			f.val.runtime.typeErrorResult(true, "prototype is not an object")
		}
	}

	return false
}

func (f *nativeFuncObject) defaultConstruct(args []Value) Value {
	proto := f.getStr("prototype")
	var protoObj *Object
	if p, ok := proto.(*Object); ok {
		protoObj = p
	} else {
		protoObj = f.val.runtime.global.ObjectPrototype
	}
	obj := f.val.runtime.newBaseObject(protoObj, classObject).val
	ret := f.f(FunctionCall{
		This:      obj,
		Arguments: args,
	})

	if ret, ok := ret.(*Object); ok {
		return ret
	}
	return obj
}

func (f *nativeFuncObject) assertCallable() (func(FunctionCall) Value, bool) {
	if f.f != nil {
		return f.f, true
	}
	return nil, false
}

func (f *nativeFuncObject) init(name string, length int) {
	f.baseObject.init()

	f.nameProp.configurable = true
	f.nameProp.value = newStringValue(name)
	f._put("name", &f.nameProp)

	f.lenProp.configurable = true
	f.lenProp.value = valueInt(length)
	f._put("length", &f.lenProp)
}

func (f *boundFuncObject) getProp(n Value) Value {
	return f.getPropStr(n.String())
}

func (f *boundFuncObject) getPropStr(name string) Value {
	if name == "caller" || name == "arguments" {
		//f.runtime.typeErrorResult(true, "'caller' and 'arguments' are restricted function properties and cannot be accessed in this context.")
		return f.val.runtime.global.throwerProperty
	}
	return f.nativeFuncObject.getPropStr(name)
}

func (f *boundFuncObject) delete(n Value, throw bool) bool {
	return f.deleteStr(n.String(), throw)
}

func (f *boundFuncObject) deleteStr(name string, throw bool) bool {
	if name == "caller" || name == "arguments" {
		return true
	}
	return f.nativeFuncObject.deleteStr(name, throw)
}

func (f *boundFuncObject) putStr(name string, val Value, throw bool) {
	if name == "caller" || name == "arguments" {
		f.val.runtime.typeErrorResult(true, "'caller' and 'arguments' are restricted function properties and cannot be accessed in this context.")
	}
	f.nativeFuncObject.putStr(name, val, throw)
}

func (f *boundFuncObject) put(n Value, val Value, throw bool) {
	f.putStr(n.String(), val, throw)
}
