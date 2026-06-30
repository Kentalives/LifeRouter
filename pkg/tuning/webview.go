package tuning

import (
	"encoding/json"
	"fmt"
	"html/template"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/Kentalives/LifeRouter/internal/grid"
)

var browserOpener = openBrowser

// OpenVisualizer writes the rasterized grid to a temporary HTML file and opens
// it in the system browser. It is intended for manual inspection of large grids;
// automated tests should validate RasterizedGrid data or renderHTML instead.
func (g RasterizedGrid) OpenVisualizer() error {
	if g.Rows == 0 || g.Cols == 0 {
		return fmt.Errorf("rows and cols must be positive")
	}

	expected := uint64(g.Rows) * uint64(g.Cols)
	if uint64(len(g.Cells)) != expected {
		return fmt.Errorf(
			"expected %d costs for %dx%d grid, got %d",
			expected,
			g.Rows,
			g.Cols,
			len(g.Cells),
		)
	}

	payload := struct {
		Costs []grid.Cost `json:"costs"`
		Rows  uint        `json:"rows"`
		Cols  uint        `json:"cols"`
	}{
		Costs: g.Cells,
		Rows:  g.Rows,
		Cols:  g.Cols,
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	html, err := renderHTML(string(data))
	if err != nil {
		return err
	}

	file, err := os.CreateTemp("", "cost-grid-*.html")
	if err != nil {
		return err
	}
	fileName := file.Name()

	if _, err := file.WriteString(html); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}

	return browserOpener(fileName)
}

// renderHTML injects the serialized grid payload into the visualizer page.
func renderHTML(jsonData string) (string, error) {
	const page = `<!doctype html>
<html>
<head>
  <meta charset="utf-8" />
  <title>Cost Grid Visualizer</title>
  <style>
    body {
      font-family: system-ui, sans-serif;
      margin: 20px;
      background: #f7f7f8;
    }

    .controls {
      margin-bottom: 12px;
      background: white;
      padding: 12px;
      border: 1px solid #ddd;
      border-radius: 10px;
    }

    .grid-shell {
      display: grid;
      grid-template-columns: 72px 1fr;
      grid-template-rows: 32px 1fr;
      height: 80vh;
      border: 1px solid #ccc;
      background: white;
    }

    .corner {
      grid-column: 1;
      grid-row: 1;
      background: #eee;
      border-right: 1px solid #ccc;
      border-bottom: 1px solid #ccc;
      font-size: 12px;
      display: flex;
      align-items: center;
      justify-content: center;
    }

    .top-header-wrap {
      grid-column: 2;
      grid-row: 1;
      overflow: hidden;
      background: #eee;
      border-bottom: 1px solid #ccc;
    }

    .left-header-wrap {
      grid-column: 1;
      grid-row: 2;
      overflow: hidden;
      background: #eee;
      border-right: 1px solid #ccc;
    }

    .main-scroll {
      grid-column: 2;
      grid-row: 2;
      overflow: auto;
    }

    canvas {
      display: block;
      image-rendering: pixelated;
    }

    #legend {
      display: flex;
      gap: 12px;
      flex-wrap: wrap;
      margin-top: 8px;
      font-family: monospace;
    }

    .legend-item {
      display: inline-flex;
      align-items: center;
      gap: 5px;
    }

    .swatch {
      width: 14px;
      height: 14px;
      border: 1px solid #999;
    }

    #message {
      margin-top: 8px;
      font-weight: 600;
    }

    #hover {
      margin-top: 8px;
      font-family: monospace;
    }
  </style>
</head>
<body>
  <div class="controls">
    <strong>Cost Grid Visualizer</strong>

    <label style="margin-left: 16px;">
      Cell size:
      <input id="cellSize" type="number" min="2" max="40" value="4" />
    </label>

    <button id="render">Render</button>

    <div id="message"></div>
    <div id="legend"></div>
  </div>

  <div class="grid-shell">
    <div class="corner">row / col</div>

    <div class="top-header-wrap">
      <canvas id="topHeader"></canvas>
    </div>

    <div class="left-header-wrap">
      <canvas id="leftHeader"></canvas>
    </div>

    <div id="mainScroll" class="main-scroll">
      <canvas id="gridCanvas"></canvas>
    </div>
  </div>

  <div id="hover">Hover a cell to see row, col, index, and value.</div>

  <script>
    const input = {{.JSON}};

    const palette = [
      "#F54927",
      "#27F557",
      "#266EE0",
      "#ca750c",
      "#e720d7"
    ];

    let cellSize = 4;
    let colorByValue = new Map();

    function sizeCanvas(canvas, width, height) {
      const dpr = window.devicePixelRatio || 1;
      canvas.style.width = width + "px";
      canvas.style.height = height + "px";
      canvas.width = Math.max(1, Math.floor(width * dpr));
      canvas.height = Math.max(1, Math.floor(height * dpr));

      const ctx = canvas.getContext("2d");
      ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
      return ctx;
    }

    function chooseLabelStep(size) {
      if (size >= 18) return 1;
      if (size >= 10) return 5;
      if (size >= 6) return 10;
      if (size >= 3) return 25;
      return 50;
    }

    function buildColors() {
      const distinct = Array.from(new Set(input.costs.map(String)));

      colorByValue = new Map();
      distinct.forEach((value, i) => {
        colorByValue.set(value, palette[i % palette.length]);
      });

      const legend = document.getElementById("legend");
      legend.innerHTML = "";

      distinct.forEach(value => {
        const item = document.createElement("span");
        item.className = "legend-item";

        const swatch = document.createElement("span");
        swatch.className = "swatch";
        swatch.style.background = colorByValue.get(value);

        const label = document.createElement("span");
        label.textContent = value;

        item.appendChild(swatch);
        item.appendChild(label);
        legend.appendChild(item);
      });

      const message = document.getElementById("message");
      message.textContent =
        "Rendered " + input.rows + "x" + input.cols +
        " grid. Distinct values: " + distinct.length + ".";

      if (distinct.length > 5) {
        message.textContent +=
          " Warning: more than 5 distinct values; colors repeat.";
      }
    }

    function drawGrid() {
      const canvas = document.getElementById("gridCanvas");

      const width = input.cols * cellSize;
      const height = input.rows * cellSize;

      const ctx = sizeCanvas(canvas, width, height);

      const drawText = cellSize >= 10;
      const drawLines = cellSize >= 6;

      ctx.clearRect(0, 0, width, height);
      ctx.font = Math.max(8, Math.floor(cellSize * 0.55)) + "px monospace";
      ctx.textAlign = "center";
      ctx.textBaseline = "middle";

      for (let r = 0; r < input.rows; r++) {
        for (let c = 0; c < input.cols; c++) {
          const i = r * input.cols + c;
          const value = input.costs[i];

          const x = c * cellSize;
          const y = r * cellSize;

          ctx.fillStyle = colorByValue.get(String(value)) || "#fff";
          ctx.fillRect(x, y, cellSize, cellSize);

          if (drawLines) {
            ctx.strokeStyle = "#e5e5e5";
            ctx.strokeRect(x, y, cellSize, cellSize);
          }

          if (drawText) {
            ctx.fillStyle = "#111";
            ctx.fillText(String(value), x + cellSize / 2, y + cellSize / 2);
          }
        }
      }
    }

    function drawHeaders() {
      const main = document.getElementById("mainScroll");
      const top = document.getElementById("topHeader");
      const left = document.getElementById("leftHeader");

      const topCtx = sizeCanvas(top, main.clientWidth, 32);
      const leftCtx = sizeCanvas(left, 72, main.clientHeight);

      topCtx.fillStyle = "#eee";
      topCtx.fillRect(0, 0, main.clientWidth, 32);

      leftCtx.fillStyle = "#eee";
      leftCtx.fillRect(0, 0, 72, main.clientHeight);

      topCtx.font = "11px monospace";
      topCtx.fillStyle = "#333";
      topCtx.textAlign = "center";
      topCtx.textBaseline = "middle";

      leftCtx.font = "11px monospace";
      leftCtx.fillStyle = "#333";
      leftCtx.textAlign = "right";
      leftCtx.textBaseline = "middle";

      const firstCol = Math.floor(main.scrollLeft / cellSize);
      const lastCol = Math.min(
        input.cols - 1,
        Math.ceil((main.scrollLeft + main.clientWidth) / cellSize)
      );

      const firstRow = Math.floor(main.scrollTop / cellSize);
      const lastRow = Math.min(
        input.rows - 1,
        Math.ceil((main.scrollTop + main.clientHeight) / cellSize)
      );

      const step = chooseLabelStep(cellSize);

      for (let c = firstCol; c <= lastCol; c++) {
        if (c % step !== 0) continue;
        const x = c * cellSize - main.scrollLeft + cellSize / 2;
        topCtx.fillText(String(c), x, 16);
      }

      for (let r = firstRow; r <= lastRow; r++) {
        if (r % step !== 0) continue;
        const y = r * cellSize - main.scrollTop + cellSize / 2;
        leftCtx.fillText(String(r), 64, y);
      }
    }

    function render() {
      cellSize = Number(document.getElementById("cellSize").value);

      if (!Number.isInteger(cellSize) || cellSize < 2 || cellSize > 40) {
        alert("Cell size must be an integer from 2 to 40.");
        return;
      }

      buildColors();
      drawGrid();
      drawHeaders();
    }

    document.getElementById("render").addEventListener("click", render);

    document.getElementById("mainScroll").addEventListener("scroll", drawHeaders);

    document.getElementById("gridCanvas").addEventListener("mousemove", event => {
      const rect = event.currentTarget.getBoundingClientRect();

      const col = Math.floor((event.clientX - rect.left) / cellSize);
      const row = Math.floor((event.clientY - rect.top) / cellSize);

      if (row < 0 || row >= input.rows || col < 0 || col >= input.cols) {
        return;
      }

      const index = row * input.cols + col;
      const value = input.costs[index];

      document.getElementById("hover").textContent =
        "row=" + row +
        ", col=" + col +
        ", index=" + index +
        ", value=" + value;
    });

    window.addEventListener("resize", drawHeaders);

    render();
  </script>
</body>
</html>`

	tmpl, err := template.New("page").Parse(page)
	if err != nil {
		return "", err
	}

	var out strings.Builder
	err = tmpl.Execute(&out, map[string]template.JS{
		"JSON": template.JS(jsonData),
	})
	if err != nil {
		return "", err
	}

	return out.String(), nil
}

// openBrowser starts the platform browser opener for path without waiting for it.
func openBrowser(path string) error {
	var cmd *exec.Cmd

	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", path)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", path)
	default:
		cmd = exec.Command("xdg-open", path)
	}

	return cmd.Start()
}
