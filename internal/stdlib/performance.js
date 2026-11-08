(function (host) {
  "use strict";

  const TypeErrorCtor = TypeError;
  const SyntaxErrorCtor = SyntaxError;
  const weakMapGet = WeakMap.prototype.get;
  const weakMapSet = WeakMap.prototype.set;
  const arrayPush = Array.prototype.push;
  const arrayFilter = Array.prototype.filter;
  const arraySort = Array.prototype.sort;
  const reflectApply = Reflect.apply;
  const numberIsFinite = Number.isFinite;
  const StringCtor = String;
  const objectDefineProperty = Object.defineProperty;
  const objectGetOwnPropertyDescriptor = Object.getOwnPropertyDescriptor;
  const performanceToken = {};
  const entryToken = {};
  const performanceStates = new WeakMap();
  const entryStates = new WeakMap();
  let entries = [];
  let timeOrigin = host.timeOrigin;

  function illegalConstructor() {
    return new TypeErrorCtor("Illegal constructor");
  }

  function getState(states, value, kind) {
    const state = reflectApply(weakMapGet, states, [value]);
    if (state === undefined) {
      throw new TypeErrorCtor(`Illegal invocation: ${kind}`);
    }
    return state;
  }

  function performanceState(value) {
    return getState(performanceStates, value, "Performance");
  }

  function entryState(value) {
    return getState(entryStates, value, "PerformanceEntry");
  }

  function entryStateOfType(value, expectedType) {
    const state = entryState(value);
    if (state.entryType !== expectedType) {
      throw new TypeErrorCtor(`Illegal invocation: Performance${expectedType === "mark" ? "Mark" : "Measure"}`);
    }
    return state;
  }

  function toDOMString(value) {
    // String(Symbol()) is a JavaScript convenience; Web IDL's DOMString
    // conversion uses ToString and throws for symbols.
    if (typeof value === "symbol") {
      throw new TypeErrorCtor("Cannot convert a Symbol value to a string");
    }
    return StringCtor(value);
  }

  function toFiniteTimestamp(value) {
    // Unary plus performs Web IDL's ToNumber conversion, including its errors
    // for Symbols and BigInts (unlike the Number() function).
    const number = +value;
    if (!reflectApply(numberIsFinite, Number, [number])) {
      throw new TypeErrorCtor("The timestamp must be a finite number");
    }
    return number;
  }

  function toDictionary(value, name) {
    if (value === undefined || value === null) return null;
    if ((typeof value !== "object" && typeof value !== "function")) {
      throw new TypeErrorCtor(`Failed to convert ${name} to a dictionary`);
    }
    return value;
  }

  function snapshotDetail(value) {
    if (value === undefined || value === null) return null;
    return host.clone(value, []);
  }

  function markOptions(value) {
    const source = toDictionary(value, "PerformanceMarkOptions");
    if (source === null) return {detail: undefined, startTime: undefined};

    // Web IDL converts dictionary members in lexicographic identifier order.
    // Undefined means the optional dictionary member was not supplied.
    const detail = source.detail;
    const rawStartTime = source.startTime;
    const startTime = rawStartTime === undefined
      ? undefined
      : toFiniteTimestamp(rawStartTime);
    return {detail, startTime};
  }

  function measureMark(value) {
    return typeof value === "number" ? toFiniteTimestamp(value) : toDOMString(value);
  }

  function measureOptions(value) {
    const source = toDictionary(value, "PerformanceMeasureOptions");
    if (source === null) {
      return {
        detail: undefined, hasDetail: false,
        start: undefined, hasStart: false,
        duration: undefined, hasDuration: false,
        end: undefined, hasEnd: false,
      };
    }

    const rawDetail = source.detail;
    const hasDetail = rawDetail !== undefined;
    const rawDuration = source.duration;
    const hasDuration = rawDuration !== undefined;
    const duration = hasDuration ? toFiniteTimestamp(rawDuration) : undefined;
    const rawEnd = source.end;
    const hasEnd = rawEnd !== undefined;
    const end = hasEnd ? measureMark(rawEnd) : undefined;
    const rawStart = source.start;
    const hasStart = rawStart !== undefined;
    const start = hasStart ? measureMark(rawStart) : undefined;
    return {detail: rawDetail, hasDetail, start, hasStart, duration, hasDuration, end, hasEnd};
  }

  function latestMark(name) {
    for (let i = entries.length - 1; i >= 0; i--) {
      const state = entryState(entries[i]);
      if (state.entryType === "mark" && state.name === name) return state.startTime;
    }
    throw new SyntaxErrorCtor(`The mark does not exist: ${name}`);
  }

  function convertMarkToTimestamp(mark) {
    if (typeof mark === "string") return latestMark(mark);
    if (mark < 0) {
      throw new TypeErrorCtor(`Mark cannot be negative: received ${mark}`);
    }
    return mark;
  }

  function entryToJSON(entry) {
    const state = entryState(entry);
    return {
      name: state.name,
      entryType: state.entryType,
      startTime: state.startTime,
      duration: state.duration,
      detail: state.detail,
    };
  }

  class PerformanceEntry {
    constructor(token, state) {
      if (token !== entryToken) throw illegalConstructor();
      reflectApply(weakMapSet, entryStates, [this, state]);
      host.cloneAs(this, "unsupported");
    }
    get name() { return entryState(this).name; }
    get entryType() { return entryState(this).entryType; }
    get startTime() { return entryState(this).startTime; }
    get duration() { return entryState(this).duration; }
    toJSON() { return entryToJSON(this); }
  }

  class PerformanceMark extends PerformanceEntry {
    constructor(markName, options = undefined) {
      if (arguments.length === 0) {
        throw new TypeErrorCtor("Failed to construct 'PerformanceMark': 1 argument required");
      }
      const name = toDOMString(markName);
      const converted = markOptions(options);
      let startTime = converted.startTime;
      if (startTime === undefined) {
        startTime = host.now();
      } else if (startTime < 0) {
        throw new TypeErrorCtor(`Mark cannot be negative: received ${startTime}`);
      }
      const detail = snapshotDetail(converted.detail);
      super(entryToken, {
        name,
        entryType: "mark",
        startTime,
        duration: 0,
        detail,
      });
    }
    get detail() { return entryStateOfType(this, "mark").detail; }
  }

  class PerformanceMeasure extends PerformanceEntry {
    constructor(token, state) {
      if (token !== entryToken) throw illegalConstructor();
      super(token, state);
    }
    get detail() { return entryStateOfType(this, "measure").detail; }
  }

  class Performance extends EventTarget {
    constructor(token) {
      if (token !== performanceToken) throw illegalConstructor();
      super();
      // EventTarget stores its listener map on an own property. Keep that
      // implementation detail out of enumeration, as Web IDL attributes are
      // inherited accessors and this object has no public own data fields.
      objectDefineProperty(this, "_listeners", {enumerable: false});
      reflectApply(weakMapSet, performanceStates, [this, {timeOrigin}]);
      host.cloneAs(this, "unsupported");
    }
    get timeOrigin() { return performanceState(this).timeOrigin; }
    now() {
      performanceState(this);
      return host.now();
    }
    toJSON() {
      const state = performanceState(this);
      return {timeOrigin: state.timeOrigin};
    }
    mark(markName, options = undefined) {
      performanceState(this);
      if (arguments.length === 0) {
        throw new TypeErrorCtor("Failed to execute 'mark': 1 argument required");
      }
      const entry = new PerformanceMark(markName, options);
      reflectApply(arrayPush, entries, [entry]);
      return entry;
    }
    measure(measureName, startOrMeasureOptions = undefined, endMark) {
      performanceState(this);
      if (arguments.length === 0) {
        throw new TypeErrorCtor("Failed to execute 'measure': 1 argument required");
      }
      const name = toDOMString(measureName);

      // Web IDL selects the dictionary union member for null and object or
      // function values. Null converts to an empty dictionary, as does the
      // optional default used for undefined; other primitives use DOMString.
      let legacyStart;
      let hasLegacyStart = false;
      let options;
      if (startOrMeasureOptions === undefined || startOrMeasureOptions === null) {
        options = measureOptions(startOrMeasureOptions);
      } else if (startOrMeasureOptions !== null &&
          (typeof startOrMeasureOptions === "object" || typeof startOrMeasureOptions === "function")) {
        options = measureOptions(startOrMeasureOptions);
      } else {
        legacyStart = toDOMString(startOrMeasureOptions);
        hasLegacyStart = true;
        options = null;
      }

      const hasEndMark = endMark !== undefined && arguments.length >= 3;
      const convertedEndMark = hasEndMark ? toDOMString(endMark) : undefined;

      if (options !== null &&
          (options.hasStart || options.hasEnd || options.hasDuration || options.hasDetail)) {
        if (hasEndMark) {
          throw new TypeErrorCtor("endMark cannot be supplied with PerformanceMeasureOptions");
        }
        if (!options.hasStart && !options.hasEnd) {
          throw new TypeErrorCtor("PerformanceMeasureOptions must include start or end");
        }
        if (options.hasStart && options.hasDuration && options.hasEnd) {
          throw new TypeErrorCtor("start, duration, and end cannot all be supplied");
        }
      }

      let endTime;
      if (hasEndMark) {
        endTime = convertMarkToTimestamp(convertedEndMark);
      } else if (options !== null && options.hasEnd) {
        endTime = convertMarkToTimestamp(options.end);
      } else if (options !== null && options.hasStart && options.hasDuration) {
        const start = convertMarkToTimestamp(options.start);
        const duration = convertMarkToTimestamp(options.duration);
        endTime = start + duration;
      } else {
        endTime = host.now();
      }

      let startTime;
      if (options !== null && options.hasStart) {
        startTime = convertMarkToTimestamp(options.start);
      } else if (options !== null && options.hasDuration && options.hasEnd) {
        const duration = convertMarkToTimestamp(options.duration);
        const end = convertMarkToTimestamp(options.end);
        startTime = end - duration;
      } else if (hasLegacyStart) {
        startTime = convertMarkToTimestamp(legacyStart);
      } else {
        startTime = 0;
      }

      let detail = null;
      if (options !== null && options.hasDetail) {
        detail = snapshotDetail(options.detail);
      }
      const entry = new PerformanceMeasure(entryToken, {
        name,
        entryType: "measure",
        startTime,
        duration: endTime - startTime,
        detail,
      });
      reflectApply(arrayPush, entries, [entry]);
      return entry;
    }
    clearMarks(markName = undefined) {
      performanceState(this);
      const filterByName = arguments.length > 0 && markName !== undefined;
      const name = filterByName ? toDOMString(markName) : undefined;
      entries = reflectApply(arrayFilter, entries, [entry => {
        const state = entryState(entry);
        return state.entryType !== "mark" || (filterByName && state.name !== name);
      }]);
    }
    clearMeasures(measureName = undefined) {
      performanceState(this);
      const filterByName = arguments.length > 0 && measureName !== undefined;
      const name = filterByName ? toDOMString(measureName) : undefined;
      entries = reflectApply(arrayFilter, entries, [entry => {
        const state = entryState(entry);
        return state.entryType !== "measure" || (filterByName && state.name !== name);
      }]);
    }
    getEntries() {
      performanceState(this);
      return sortedEntries(null, null);
    }
    getEntriesByType(type) {
      performanceState(this);
      if (arguments.length === 0) {
        throw new TypeErrorCtor("Failed to execute 'getEntriesByType': 1 argument required");
      }
      return sortedEntries(null, toDOMString(type));
    }
    getEntriesByName(name, type = undefined) {
      performanceState(this);
      if (arguments.length === 0) {
        throw new TypeErrorCtor("Failed to execute 'getEntriesByName': 1 argument required");
      }
      const convertedName = toDOMString(name);
      const convertedType = type === undefined ? null : toDOMString(type);
      return sortedEntries(convertedName, convertedType);
    }
  }

  function sortedEntries(name, type) {
    const result = [];
    for (const entry of entries) {
      const state = entryState(entry);
      if (name !== null && state.name !== name) continue;
      if (type !== null && state.entryType !== type) continue;
      reflectApply(arrayPush, result, [entry]);
    }
    return reflectApply(arraySort, result, [(a, b) => {
      const difference = entryState(a).startTime - entryState(b).startTime;
      return difference;
    }]);
  }

  function exposeWebIDLProperties(prototype, names) {
    for (const name of names) {
      const descriptor = objectGetOwnPropertyDescriptor(prototype, name);
      if (descriptor === undefined) continue;
      descriptor.enumerable = true;
      objectDefineProperty(prototype, name, descriptor);
    }
  }

  // Interface operations and attributes are enumerable in Web IDL. Class
  // syntax makes them non-enumerable by default, so set the binding shape
  // explicitly. These functions have no exposed constructor arguments except
  // PerformanceMark's required markName.
  exposeWebIDLProperties(Performance.prototype, [
    "timeOrigin", "now", "toJSON", "mark", "measure", "clearMarks",
    "clearMeasures", "getEntries", "getEntriesByType", "getEntriesByName",
  ]);
  exposeWebIDLProperties(PerformanceEntry.prototype, [
    "name", "entryType", "startTime", "duration", "toJSON",
  ]);
  exposeWebIDLProperties(PerformanceMark.prototype, ["detail"]);
  exposeWebIDLProperties(PerformanceMeasure.prototype, ["detail"]);
  objectDefineProperty(Performance, "length", {value: 0});
  objectDefineProperty(PerformanceEntry, "length", {value: 0});
  objectDefineProperty(PerformanceMeasure, "length", {value: 0});

  objectDefineProperty(Performance.prototype, Symbol.toStringTag, {
    value: "Performance", configurable: true,
  });
  objectDefineProperty(PerformanceEntry.prototype, Symbol.toStringTag, {
    value: "PerformanceEntry", configurable: true,
  });
  objectDefineProperty(PerformanceMark.prototype, Symbol.toStringTag, {
    value: "PerformanceMark", configurable: true,
  });
  objectDefineProperty(PerformanceMeasure.prototype, Symbol.toStringTag, {
    value: "PerformanceMeasure", configurable: true,
  });

  const performance = new Performance(performanceToken);
  host.cloneAs(performance, "unsupported");

  return {performance, Performance, PerformanceEntry, PerformanceMark, PerformanceMeasure};
})
