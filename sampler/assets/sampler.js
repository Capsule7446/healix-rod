(function () {
  if (window.__healixSamplerStarted) return;
  window.__healixSamplerStarted = true;
  var captureProtocolVersion = "1.0";
  var knownNodes = new WeakMap();
  var activeAntSelectRoot = null;
  var pendingAntSelect = null;
  var pendingInputs = new Map();
  var lastInputValues = new WeakMap();
  var pendingCaptures = new Set();
  var captureEnabled = window.__healixSamplerInitialCaptureEnabled !== false;
  // Validation sampling is deliberately independent from normal action
  // capture.  While armed, the next left pointer sequence is consumed in the
  // capture phase before application handlers can observe it.
  var validationArmed = false;
  var validationPointerPending = false;
  var validationOverlay = null;

  function normalize(value) {
    return String(value || "").replace(/\s+/g, " ").trim();
  }

  function uuid() {
    if (window.crypto && typeof window.crypto.randomUUID === "function") {
      return window.crypto.randomUUID();
    }
    return Date.now().toString(36) + "-" + Math.random().toString(36).slice(2);
  }

  function cssPath(el) {
    if (!el || el.nodeType !== 1) return "";
    var preferred = preferredCSSSelector(el);
    if (preferred) return preferred;
    return fullCSSPath(el);
  }

  function selectorMatchesOnly(selector, el) {
    try {
      var found = document.querySelectorAll(selector);
      return found.length === 1 && found[0] === el;
    } catch (e) {
      return false;
    }
  }

  function preferredCSSSelector(el) {
    if (el.id) {
      var idSelector = "#" + CSS.escape(el.id);
      if (selectorMatchesOnly(idSelector, el)) return idSelector;
    }
    var testID = el.getAttribute("data-testid");
    if (testID) {
      var testIDSelector = "[data-testid=" + JSON.stringify(testID) + "]";
      if (selectorMatchesOnly(testIDSelector, el)) return testIDSelector;
    }
    var name = el.getAttribute("name");
    if (name) {
      var nameSelector = el.tagName.toLowerCase() + "[name=" + JSON.stringify(name) + "]";
      if (selectorMatchesOnly(nameSelector, el)) return nameSelector;
    }
    var aria = el.getAttribute("aria-label");
    if (aria) {
      var ariaSelector = el.tagName.toLowerCase() + "[aria-label=" + JSON.stringify(aria) + "]";
      if (selectorMatchesOnly(ariaSelector, el)) return ariaSelector;
    }
    return "";
  }

  function cssSegment(el) {
    var segment = el.tagName.toLowerCase();
    if (el.id) segment += "#" + CSS.escape(el.id);
    var parent = el.parentElement;
    if (parent) {
      var siblings = Array.prototype.filter.call(parent.children, function (candidate) {
        return candidate.tagName === el.tagName;
      });
      if (siblings.length > 1) {
        segment += ":nth-of-type(" + (siblings.indexOf(el) + 1) + ")";
      }
    }
    return segment;
  }

  function fullCSSPath(el) {
    var parts = [];
    var current = el;
    while (current && current.nodeType === 1) {
      parts.unshift(cssSegment(current));
      current = current.parentElement;
    }
    return parts.join(" > ");
  }

  function shortUniqueCSSPath(el) {
    var parts = [];
    var current = el;
    while (current && current.nodeType === 1) {
      parts.unshift(cssSegment(current));
      var candidate = parts.join(" > ");
      if (selectorMatchesOnly(candidate, el)) return candidate;
      current = current.parentElement;
    }
    return "";
  }

  function implicitRole(el) {
    var explicit = el.getAttribute("role");
    if (explicit) return normalize(explicit).split(" ")[0].toLowerCase();
    var tag = el.tagName.toLowerCase();
    if (tag === "a" && el.hasAttribute("href")) return "link";
    if (tag === "button") return "button";
    if (tag === "textarea") return "textbox";
    if (tag === "select") return el.multiple || el.size > 1 ? "listbox" : "combobox";
    if (tag === "img" && el.getAttribute("alt") !== "") return "img";
    if (/^h[1-6]$/.test(tag)) return "heading";
    if (tag === "input") {
      var type = (el.getAttribute("type") || "text").toLowerCase();
      if (["button", "image", "reset", "submit"].includes(type)) return "button";
      if (type === "checkbox") return "checkbox";
      if (type === "radio") return "radio";
      if (type === "range") return "slider";
      if (type === "number") return "spinbutton";
      if (type === "search") return "searchbox";
      if (!["hidden", "color", "file"].includes(type)) return "textbox";
    }
    return "";
  }

  function labelText(el) {
    if (el.labels && el.labels.length) {
      return normalize(Array.prototype.map.call(el.labels, function (label) {
        return label.innerText || label.textContent;
      }).join(" "));
    }
    var labelledBy = el.getAttribute("aria-labelledby");
    if (labelledBy) {
      return normalize(labelledBy.split(/\s+/).map(function (id) {
        var ref = document.getElementById(id);
        return ref ? ref.innerText || ref.textContent : "";
      }).join(" "));
    }
    var wrapping = el.closest("label");
    return wrapping ? normalize(wrapping.innerText || wrapping.textContent) : "";
  }

  function accessibleName(el) {
    var labelledBy = el.getAttribute("aria-labelledby");
    if (labelledBy) {
      var referenced = normalize(labelledBy.split(/\s+/).map(function (id) {
        var ref = document.getElementById(id);
        return ref ? ref.innerText || ref.textContent : "";
      }).join(" "));
      if (referenced) return referenced;
    }
    var aria = normalize(el.getAttribute("aria-label"));
    if (aria) return aria;
    var labelled = labelText(el);
    if (labelled) return labelled;
    if (el.tagName.toLowerCase() === "img") return normalize(el.getAttribute("alt"));
    if (el.tagName.toLowerCase() === "input") {
      var type = (el.type || "").toLowerCase();
      if (["button", "reset", "submit"].includes(type)) return normalize(el.value);
      var placeholder = normalize(el.getAttribute("placeholder"));
      if (placeholder) return placeholder;
    }
    var title = normalize(el.getAttribute("title"));
    if (title) return title;
    var text = normalize(el.innerText || el.textContent);
    if (text) return text;
    var labelledDescendant = el.querySelector("[aria-label]");
    return labelledDescendant ? normalize(labelledDescendant.getAttribute("aria-label")) : "";
  }

  function selectors(el, role, name) {
    var result = [];
    function add(type, value) {
      if (!value || result.some(function (item) { return item.type === type && item.value === value; })) return;
      result.push({ type: type, value: value, priority: result.length });
    }
    // A bare role such as "textbox" is commonly ambiguous. It can silently
    // resolve a password action to the username input, so only emit semantic
    // role selectors when an accessible name disambiguates the control.
    if (role && name && roleSelectorMatchesOnly(role, name, el)) {
      add("role", role + "[name=" + JSON.stringify(name) + "]");
    }
    var testID = el.getAttribute("data-testid");
    if (testID && selectorMatchesOnly("[data-testid=" + JSON.stringify(testID) + "]", el)) {
      add("testid", testID);
    }
    add("css", cssPath(el));
    add("css", shortUniqueCSSPath(el));
    add("css", fullCSSPath(el));
    return result;
  }

  function roleSelectorMatchesOnly(role, name, el) {
    var matches = Array.prototype.filter.call(document.querySelectorAll("*"), function (candidate) {
      return implicitRole(candidate) === role && accessibleName(candidate) === name;
    });
    if (matches.length !== 1) return false;
    var match = matches[0];
    return match === el || el.contains(match) || match.closest(".ant-select") === el;
  }

  function ancestorPath(el) {
    var path = [];
    var current = el;
    while (current && current.nodeType === 1) {
      var segment = current.tagName.toLowerCase();
      if (current.id) segment += "#" + current.id;
      path.unshift(segment);
      current = current.parentElement;
    }
    return path;
  }

  function collect(el, overrides) {
    overrides = overrides || {};
    var attributes = {};
    Array.prototype.forEach.call(el.attributes || [], function (attribute) {
      if (attribute.name !== "style") attributes[attribute.name] = attribute.value;
    });
    var parent = el.parentElement;
    var siblings = parent ? Array.prototype.slice.call(parent.children) : [el];
    var index = Math.max(0, siblings.indexOf(el));
    var role = overrides.role || implicitRole(el);
    var name = overrides.name || accessibleName(el);
    var form = el.closest("form");
    var foundSelectors = selectors(el, role, name);
    var identitySelector = foundSelectors.find(function (item) { return item.type === "testid"; }) ||
      foundSelectors.find(function (item) { return item.type === "css" && item.value.charAt(0) === "#"; }) ||
      foundSelectors.find(function (item) { return item.type === "css"; }) || foundSelectors[0];
    return {
      identity_key: location.origin + location.pathname + "|" + identitySelector.type + ":" + identitySelector.value,
      node: {
        id: "pending",
        role: role,
        selectors: foundSelectors,
        fingerprint: {
          tag: el.tagName.toLowerCase(),
          attributes: attributes,
          text: normalize(el.innerText || el.textContent),
          aria: { role: role, name: name },
          path: ancestorPath(el),
          sibling_index: index,
          neighbors: {
            prev: index > 0 ? siblings[index - 1].tagName.toLowerCase() : "",
            next: index + 1 < siblings.length ? siblings[index + 1].tagName.toLowerCase() : "",
            parent_tag: parent ? parent.tagName.toLowerCase() : ""
          },
          label_text: labelText(el),
          form_id: form ? form.id || form.getAttribute("name") || "" : ""
        }
      }
    };
  }

  function eventPath(event) {
    if (typeof event.composedPath === "function") return event.composedPath();
    var path = [];
    var current = event.target;
    while (current) {
      path.push(current);
      current = current.parentNode || current.host || null;
    }
    path.push(window);
    return path;
  }

  function closestFromPath(path, selector) {
    for (var i = 0; i < path.length; i++) {
      if (!path[i] || path[i].nodeType !== 1) continue;
      if (path[i].matches(selector)) return path[i];
      var closest = path[i].closest(selector);
      if (closest) return closest;
    }
    return null;
  }

  function antSelectRoot(path) {
    var root = closestFromPath(path, ".ant-select");
    return root && !root.classList.contains("ant-select-disabled") ? root : null;
  }

  function antSelectOption(path) {
    var option = closestFromPath(path, ".ant-select-item-option");
    if (!option || option.classList.contains("ant-select-item-option-disabled")) return null;
    return option;
  }

  function activeAntSelect() {
    if (activeAntSelectRoot && activeAntSelectRoot.isConnected) return activeAntSelectRoot;
    return document.querySelector(".ant-select.ant-select-open");
  }

  function antSelectName(root) {
    var labelled = labelText(root);
    if (labelled) return labelled;
    var input = root.querySelector("[role=combobox], input");
    if (input) {
      var aria = normalize(input.getAttribute("aria-label"));
      if (aria) return aria;
      var placeholder = normalize(input.getAttribute("placeholder"));
      if (placeholder) return placeholder;
    }
    var placeholderEl = root.querySelector(".ant-select-selection-placeholder");
    if (placeholderEl) {
      var placeholderText = normalize(placeholderEl.innerText || placeholderEl.textContent);
      if (placeholderText) return placeholderText;
    }
    return accessibleName(root);
  }

  function optionText(option) {
    var content = option.querySelector(".ant-select-item-option-content") || option;
    return normalize(content.innerText || content.textContent);
  }

  function actionHints(el, kind) {
    if (kind !== "click" || !el) return {};
    var name = normalize(accessibleName(el)).toLowerCase();
    var text = normalize(el.innerText || el.textContent).toLowerCase();
    var className = String(el.getAttribute("class") || "").toLowerCase();
    var optionalNames = {
      "close": true, "关闭": true,
      "知道了": true, "我知道了": true,
      "稍后": true, "稍后再说": true,
      "跳过": true, "skip": true, "later": true
    };
    var antClose = className.indexOf("ant-modal-close") !== -1 ||
      className.indexOf("ant-drawer-close") !== -1 ||
      className.indexOf("ant-notification-notice-close") !== -1 ||
      className.indexOf("ant-message-notice-close") !== -1 ||
      className.indexOf("ant-tour-close") !== -1 ||
      el.closest(".ant-modal-close,.ant-drawer-close,.ant-notification-notice-close,.ant-message-notice-close,.ant-tour-close");
    if (optionalNames[name] || optionalNames[text] || antClose) {
      return { optional: true, intent: "close_overlay" };
    }
    return {};
  }

  function appendPendingAntSelect(root, value, collectOptions) {
    if (!pendingAntSelect || pendingAntSelect.el !== root) {
      flushPendingAntSelect();
      pendingAntSelect = { el: root, values: [], collect: collectOptions, timer: null };
    }
    if (!pendingAntSelect.values.includes(value)) pendingAntSelect.values.push(value);
    schedulePendingAntSelectFlush();
  }

  function schedulePendingAntSelectFlush() {
    if (!pendingAntSelect) return;
    if (pendingAntSelect.timer) clearTimeout(pendingAntSelect.timer);
    pendingAntSelect.timer = setTimeout(function () {
      flushPendingAntSelect();
    }, 900);
  }

  function flushPendingAntSelect() {
    if (!pendingAntSelect) return Promise.resolve();
    var pending = pendingAntSelect;
    pendingAntSelect = null;
    if (pending.timer) clearTimeout(pending.timer);
    if (pending.values.length === 0) return Promise.resolve();
    return emit("select", pending.values.length === 1 ? pending.values[0] : pending.values, pending.el, pending.collect);
  }

  var controlAdapters = [
    {
      name: "ant-select",
      capture: function (eventType, event) {
        if (eventType !== "click") return undefined;
        var path = eventPath(event);
        var option = antSelectOption(path);
        if (option) {
          var root = activeAntSelect();
          if (!root) return null;
          var name = antSelectName(root);
          var collectOptions = { role: "combobox", name: name };
          if (root.classList.contains("ant-select-multiple")) {
            appendPendingAntSelect(root, optionText(option), collectOptions);
            return null;
          }
          activeAntSelectRoot = null;
          return {
            kind: "select",
            value: optionText(option),
            el: root,
            collect: collectOptions
          };
        }
        var root = antSelectRoot(path);
        if (root) {
          activeAntSelectRoot = root;
          return null;
        }
        return undefined;
      }
    }
  ];

  function targetOf(event) {
    var path = eventPath(event);
    var actionableRoles = new Set([
      "button", "link", "checkbox", "radio", "switch", "tab", "menuitem",
      "menuitemcheckbox", "menuitemradio", "option", "combobox", "textbox",
      "searchbox", "spinbutton", "slider", "treeitem"
    ]);
    var customControl = null;
    var svgCustomControl = null;
    for (var i = 0; i < path.length; i++) {
      if (!path[i] || path[i].nodeType !== 1) continue;
      var candidate = path[i];
      if (window.getComputedStyle(candidate).pointerEvents === "none") continue;
      if (candidate.matches("button, a[href], input, select, textarea, summary, [contenteditable=true]")) {
        return candidate;
      }
      var role = implicitRole(candidate);
      if (actionableRoles.has(role)) return candidate;
      if (!customControl && (
        candidate.hasAttribute("data-testid") ||
        candidate.hasAttribute("onclick") ||
        candidate.tabIndex >= 0 ||
        window.getComputedStyle(candidate).cursor === "pointer"
      )) {
        if (!customControl && candidate instanceof HTMLElement) {
          customControl = candidate;
        } else if (!svgCustomControl) {
          svgCustomControl = candidate;
        }
      }
    }
    return customControl || svgCustomControl;
  }

  function validationTargetOf(event) {
    var direct = targetOf(event);
    if (direct) return direct;
    var path = eventPath(event);
    for (var i = 0; i < path.length; i++) {
      if (!path[i] || path[i].nodeType !== 1) continue;
      var candidate = path[i];
      if (candidate.matches("[data-healix-validation-overlay], script, style")) continue;
      var textContainer = candidate.closest("p,li,td,th,h1,h2,h3,h4,h5,h6,label,blockquote,pre,[role=alert],[role=status]");
      if (textContainer && normalize(textContainer.innerText || textContainer.textContent)) return textContainer;
    }
    return null;
  }

  function isActuallyVisible(el) {
    if (!el || !el.isConnected) return false;
    var style = window.getComputedStyle(el);
    if (style.display === "none" || style.visibility === "hidden" || Number(style.opacity) === 0) return false;
    var rect = el.getBoundingClientRect();
    return rect.width > 0 && rect.height > 0;
  }

  function stateBool(el, property, aria) {
    if (typeof el[property] === "boolean") return el[property];
    var value = normalize(el.getAttribute(aria)).toLowerCase();
    return value === "true" || value === "mixed";
  }

  function validationSemantics(el) {
    var tag = el.tagName.toLowerCase();
    var role = implicitRole(el);
    var inputType = tag === "input" ? String(el.type || "text").toLowerCase() : "";
    var typeText = inputType === "password" || /password|token|secret/i.test(String(el.name || "") + " " + String(el.id || ""));
    var text = normalize(el.innerText || el.textContent);
    var result = { kind: "visible", expected: "", actual: String(isActuallyVisible(el)), supported_kinds: ["exists", "not_exists", "visible", "not_visible"] };

    if (typeText) {
      return { kind: "value_not_empty", expected: "", actual: String(!!String(el.value || "")), sensitive: true,
        supported_kinds: ["exists", "value_not_empty"] };
    }
    if (inputType === "checkbox" || inputType === "radio" || role === "checkbox" || role === "radio" || role === "switch") {
      var mixed = normalize(el.getAttribute("aria-checked")).toLowerCase() === "mixed";
      var checked = stateBool(el, "checked", "aria-checked");
      return { kind: mixed ? "mixed" : checked ? "checked" : "unchecked", expected: "", actual: mixed ? "mixed" : String(checked),
        supported_kinds: ["checked", "unchecked", "mixed", "enabled", "disabled", "visible", "not_visible"] };
    }
    if (role === "tab" || role === "option" || role === "treeitem") {
      var selected = normalize(el.getAttribute("aria-selected")).toLowerCase() === "true" || !!el.selected;
      return { kind: selected ? "selected" : "unselected", expected: "", actual: String(selected),
        supported_kinds: ["selected", "unselected", "enabled", "disabled", "visible", "not_visible"] };
    }
    if (role === "button" && el.hasAttribute("aria-pressed")) {
      var pressed = normalize(el.getAttribute("aria-pressed")).toLowerCase() === "true";
      return { kind: pressed ? "pressed" : "unpressed", expected: "", actual: String(pressed),
        supported_kinds: ["pressed", "unpressed", "enabled", "disabled", "visible", "not_visible"] };
    }
    if (tag === "select") {
      var selectedOptions = Array.prototype.map.call(el.selectedOptions || [], function (option) { return normalize(option.text); });
      if (el.multiple) {
        return { kind: "selected_set_equals", expected: JSON.stringify(selectedOptions), actual: JSON.stringify(selectedOptions),
          supported_kinds: ["selected_set_equals", "selected_set_contains", "enabled", "disabled", "visible", "not_visible"] };
      }
      var selectedText = selectedOptions[0] || "";
      return { kind: "selected_text_equals", expected: selectedText, actual: selectedText,
        supported_kinds: ["selected_text_equals", "selected_text_contains", "selected_value_equals", "selected_value_contains", "enabled", "disabled", "visible", "not_visible"] };
    }
    if (isTextEntry(el)) {
      var value = String(el.value || el.textContent || "");
      return { kind: "value_equals", expected: value, actual: value,
        supported_kinds: ["value_equals", "value_contains", "value_matches", "value_not_empty", "enabled", "disabled", "visible", "not_visible"] };
    }
    if (text) {
      var hasTextChildren = Array.prototype.some.call(el.children || [], function (child) { return normalize(child.innerText || child.textContent); });
      return { kind: hasTextChildren ? "text_contains" : "text_equals", expected: text, actual: text,
        supported_kinds: ["text_equals", "text_contains", "text_matches", "enabled", "disabled", "visible", "not_visible", "attribute_equals", "attribute_contains"] };
    }
    var disabled = !!el.disabled || normalize(el.getAttribute("aria-disabled")).toLowerCase() === "true";
    return { kind: disabled ? "disabled" : "enabled", expected: "", actual: String(!disabled),
      supported_kinds: ["enabled", "disabled", "visible", "not_visible", "exists", "not_exists", "attribute_equals", "attribute_contains"] };
  }

  function ensureValidationOverlay() {
    if (validationOverlay && validationOverlay.isConnected) return validationOverlay;
    validationOverlay = document.createElement("div");
    validationOverlay.setAttribute("data-healix-validation-overlay", "");
    validationOverlay.style.cssText = "position:fixed;z-index:2147483647;pointer-events:none;border:2px solid #5b5ce2;background:rgba(91,92,226,.12);box-sizing:border-box;display:none;";
    document.documentElement.appendChild(validationOverlay);
    return validationOverlay;
  }

  function highlightValidationTarget(el) {
    if (!validationArmed || !el) return;
    var overlay = ensureValidationOverlay();
    var rect = el.getBoundingClientRect();
    overlay.style.left = rect.left + "px";
    overlay.style.top = rect.top + "px";
    overlay.style.width = rect.width + "px";
    overlay.style.height = rect.height + "px";
    overlay.style.display = rect.width > 0 && rect.height > 0 ? "block" : "none";
    overlay.title = el.tagName.toLowerCase() + " · " + validationSemantics(el).kind;
  }

  function clearValidationOverlay() {
    if (validationOverlay) validationOverlay.style.display = "none";
  }

  function emitValidation(el) {
    if (!el || typeof window.__healixCaptureNode !== "function") return Promise.resolve();
    var sampled = collect(el);
    var semantic = validationSemantics(el);
    var request = window.__healixCaptureNode({
      protocol_version: captureProtocolVersion,
      capture_id: uuid(), node_uuid: knownNodes.get(el) || "", identity_key: sampled.identity_key, page_url: location.href,
      action: { kind: "validate", validation: semantic }, node: sampled.node
    }).then(function (result) {
      if (result && result.node_uuid) knownNodes.set(el, result.node_uuid);
    }).catch(function () { return null; });
    return trackCapture(request);
  }

  function consumeValidationPointer(event) {
    if (!validationArmed && !validationPointerPending) return;
    if (event.type === "pointerdown" && !validationArmed) return;
    if (event.type === "pointerdown" && event.button !== 0) return;
    event.preventDefault();
    event.stopImmediatePropagation();
    event.stopPropagation();
    if (event.type !== "pointerdown") {
      if (event.type === "click") validationPointerPending = false;
      return;
    }
    var target = validationTargetOf(event);
    if (!target) return;
    validationArmed = false;
    validationPointerPending = true;
    clearValidationOverlay();
    emitValidation(target);
  }

  function onValidationPointerMove(event) {
    if (!validationArmed) return;
    highlightValidationTarget(validationTargetOf(event));
  }

  function captureIntent(eventType, event) {
    for (var i = 0; i < controlAdapters.length; i++) {
      var intent = controlAdapters[i].capture(eventType, event);
      if (intent !== undefined) return intent;
    }
    return defaultIntent(eventType, event);
  }

  function defaultIntent(eventType, event) {
    var el = targetOf(event);
    if (!el) return null;
    var tag = el.tagName.toLowerCase();
    if (eventType === "click") {
      if (isTextEntry(el) || tag === "select") return null;
      return { kind: "click", value: "", el: el, hints: actionHints(el, "click") };
    }
    if (eventType !== "change") return null;
    if (tag === "select") {
      if (el.multiple) {
        var selected = Array.prototype.map.call(el.selectedOptions || [], function (option) {
          return normalize(option.text);
        });
        return { kind: "select", value: selected, el: el };
      }
      var option = el.options && el.selectedIndex >= 0 ? el.options[el.selectedIndex] : null;
      return { kind: "select", value: option ? normalize(option.text) : "", el: el };
    }
    if (tag === "input" && (el.type || "").toLowerCase() === "file") {
      return { kind: "input", value: "${FILE}", el: el };
    }
    if (!isTextEntry(el)) return null;
    var type = (el.type || "").toLowerCase();
    var value = type === "password" ? "${PASSWORD}" : String(el.value || el.textContent || "");
    return { kind: "input", value: value, el: el };
  }

  function inputActionValue(el) {
    var type = (el.type || "").toLowerCase();
    if (type === "password") return "${PASSWORD}";
    if (type === "file") return "${FILE}";
    return String(el.value || el.textContent || "");
  }

  function queuePendingInput(el) {
    if (!el || !isTextEntry(el)) return;
    var previous = pendingInputs.get(el);
    if (previous && previous.timer) clearTimeout(previous.timer);
    var pending = { timer: null };
    pending.timer = setTimeout(function () { flushPendingInput(el); }, 350);
    pendingInputs.set(el, pending);
  }

  function flushPendingInput(el) {
    var pending = pendingInputs.get(el);
    if (pending && pending.timer) clearTimeout(pending.timer);
    pendingInputs.delete(el);
    if (!el || !el.isConnected) return Promise.resolve();
    var value = inputActionValue(el);
    if (lastInputValues.get(el) === value) return Promise.resolve();
    lastInputValues.set(el, value);
    return emit("input", value, el);
  }

  function flushPendingInputs() {
    return Promise.all(Array.from(pendingInputs.keys()).map(flushPendingInput));
  }

  function emit(kind, value, el, collectOptions, hints) {
    if (!captureEnabled) return Promise.resolve();
    if (!el || typeof window.__healixCaptureNode !== "function") return Promise.resolve();
    var sampled = collect(el, collectOptions);
    var action = Array.isArray(value)
      ? { kind: kind, value: value, values: value }
      : { kind: kind, value: value || "" };
    if (hints && (hints.optional || hints.intent)) action.hints = hints;
    var request = window.__healixCaptureNode({
      protocol_version: captureProtocolVersion,
      capture_id: uuid(),
      node_uuid: knownNodes.get(el) || "",
      identity_key: sampled.identity_key,
      page_url: location.href,
      action: action,
      node: sampled.node
    }).then(function (result) {
      if (result && result.node_uuid) knownNodes.set(el, result.node_uuid);
    }).catch(function () { return null; });
    return trackCapture(request);
  }

  function trackCapture(request) {
    pendingCaptures.add(request);
    request.then(function () { pendingCaptures.delete(request); }, function () { pendingCaptures.delete(request); });
    return request;
  }

  function emitPress(key) {
    if (!captureEnabled) return Promise.resolve();
    if (typeof window.__healixCaptureNode !== "function") return Promise.resolve();
    return trackCapture(window.__healixCaptureNode({
      protocol_version: captureProtocolVersion,
      capture_id: uuid(),
      page_url: location.href,
      action: { kind: "press", value: key }
    }).catch(function () { return null; }));
  }

  function isTextEntry(el) {
    var tag = el.tagName.toLowerCase();
    if (tag === "textarea" || el.isContentEditable) return true;
    if (tag !== "input") return false;
    return !["button", "checkbox", "color", "file", "hidden", "image", "radio", "range", "reset", "submit"].includes((el.type || "text").toLowerCase());
  }

  function onClick(event) {
    if (!captureEnabled) return;
    flushPendingInputs();
    var intent = captureIntent("click", event);
    if (!intent) return;
    emit(intent.kind, intent.value, intent.el, intent.collect, intent.hints);
  }

  function onChange(event) {
    if (!captureEnabled) return;
    var intent = captureIntent("change", event);
    if (!intent) return;
    if (intent.kind === "input" && isTextEntry(intent.el)) {
      flushPendingInput(intent.el);
      return;
    }
    emit(intent.kind, intent.value, intent.el, intent.collect, intent.hints);
  }

  function onInput(event) {
    if (!captureEnabled) return;
    queuePendingInput(targetOf(event));
  }

  function onKeyDown(event) {
    if (!captureEnabled) return;
    if (validationArmed && (event.key === "Escape" || event.code === "Escape")) {
      event.preventDefault();
      event.stopImmediatePropagation();
      validationArmed = false;
      clearValidationOverlay();
      return;
    }
    if (event.key === "Escape" || event.code === "Escape") {
      flushPendingAntSelect();
      flushPendingInputs();
      activeAntSelectRoot = null;
      emitPress("Escape");
      return;
    }
    var target = targetOf(event);
    if ((event.key === "Enter" || event.code === "Enter") && target &&
      target.tagName.toLowerCase() === "input" && isTextEntry(target)) {
      flushPendingInputs();
      emitPress("Enter");
    }
  }

  // These listeners must be registered before normal capture listeners and in
  // the capture phase: a validation click may target a link or a control with
  // pointerdown side effects, so suppressing only click is unsafe.
  document.addEventListener("pointerdown", consumeValidationPointer, true);
  document.addEventListener("mousedown", consumeValidationPointer, true);
  document.addEventListener("pointerup", consumeValidationPointer, true);
  document.addEventListener("mouseup", consumeValidationPointer, true);
  document.addEventListener("click", consumeValidationPointer, true);
  document.addEventListener("pointermove", onValidationPointerMove, true);
  document.addEventListener("click", onClick, true);
  document.addEventListener("change", onChange, true);
  document.addEventListener("input", onInput, true);
  document.addEventListener("keydown", onKeyDown, true);
  window.__healixSamplerSetCaptureEnabled = async function (enabled) {
    if (!enabled && captureEnabled) {
      await flushPendingAntSelect();
      await flushPendingInputs();
      await Promise.all(Array.from(pendingCaptures));
    }
    captureEnabled = !!enabled;
    if (!captureEnabled) {
      validationArmed = false;
      validationPointerPending = false;
      clearValidationOverlay();
    }
    if (captureEnabled) lastInputValues = new WeakMap();
    return captureEnabled;
  };
  window.__healixSamplerSetValidationArmed = function (armed) {
    if (!captureEnabled) throw new Error("sampling is paused");
    validationArmed = !!armed;
    validationPointerPending = false;
    if (!validationArmed) clearValidationOverlay();
    return validationArmed;
  };
  window.__healixSamplerStop = async function () {
    validationArmed = false;
    validationPointerPending = false;
    clearValidationOverlay();
    document.removeEventListener("pointerdown", consumeValidationPointer, true);
    document.removeEventListener("mousedown", consumeValidationPointer, true);
    document.removeEventListener("pointerup", consumeValidationPointer, true);
    document.removeEventListener("mouseup", consumeValidationPointer, true);
    document.removeEventListener("click", consumeValidationPointer, true);
    document.removeEventListener("pointermove", onValidationPointerMove, true);
    document.removeEventListener("click", onClick, true);
    document.removeEventListener("change", onChange, true);
    document.removeEventListener("input", onInput, true);
    document.removeEventListener("keydown", onKeyDown, true);
    await flushPendingAntSelect();
    await flushPendingInputs();
    await Promise.all(Array.from(pendingCaptures));
    delete window.__healixSamplerSetCaptureEnabled;
    delete window.__healixSamplerSetValidationArmed;
    window.__healixSamplerStarted = false;
  };
})();
